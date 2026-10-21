// Package bot is the store's Telegram bot: new-order notifications with
// confirm/cancel buttons, a link to the admin Mini App and a daily summary.
// It long-polls Telegram from the API process.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

// Config configures the bot.
type Config struct {
	Token string
	// AdminURL is the Mini App URL; web_app buttons need HTTPS, so other URLs disable them.
	AdminURL string
	// WebURL is the storefront, shown to users who are not admins.
	WebURL string
	// ServerURL overrides the Telegram Bot API endpoint (tests).
	ServerURL string
	Location  *time.Location
	// Daily summary time in Location.
	ReportHour, ReportMinute int
}

// Bot sends and handles Telegram messages.
type Bot struct {
	api      *tgbot.Bot
	store    *repo.Store
	admin    *service.Admin
	log      *slog.Logger
	cfg      Config
	adminURL string // empty unless HTTPS
	events   chan event

	retryDelay time.Duration
	attempts   int
	now        func() time.Time
}

type event struct {
	orderID int64
	created bool
}

var _ service.OrderEvents = (*Bot)(nil)

// New creates the bot without contacting Telegram (startup must not depend on it).
func New(cfg Config, store *repo.Store, admin *service.Admin, log *slog.Logger) (*Bot, error) {
	b := &Bot{
		store:      store,
		admin:      admin,
		log:        log,
		cfg:        cfg,
		events:     make(chan event, 1024),
		retryDelay: time.Second,
		attempts:   4,
		now:        time.Now,
	}
	if strings.HasPrefix(cfg.AdminURL, "https://") {
		b.adminURL = strings.TrimRight(cfg.AdminURL, "/")
	} else {
		log.Warn("ADMIN_URL is not HTTPS: Mini App buttons are disabled in the bot", slog.String("admin_url", cfg.AdminURL))
	}

	opts := []tgbot.Option{
		tgbot.WithSkipGetMe(),
		tgbot.WithAllowedUpdates(tgbot.AllowedUpdates{"message", "callback_query"}),
		tgbot.WithErrorsHandler(func(err error) { log.Warn("telegram polling", slog.Any("error", err)) }),
		tgbot.WithDefaultHandler(func(context.Context, *tgbot.Bot, *models.Update) {}),
	}
	if cfg.ServerURL != "" {
		opts = append(opts, tgbot.WithServerURL(cfg.ServerURL))
	}
	api, err := tgbot.New(cfg.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("telegram bot: %w", err)
	}
	api.RegisterHandler(tgbot.HandlerTypeMessageText, "start", tgbot.MatchTypeCommandStartOnly, b.handleStart)
	api.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, "", tgbot.MatchTypePrefix, b.handleCallback)
	b.api = api
	return b, nil
}

// Run polls Telegram, delivers notifications and sends the daily summary until ctx ends.
func (b *Bot) Run(ctx context.Context) {
	// getUpdates fails while a webhook is set.
	if _, err := b.api.DeleteWebhook(ctx, &tgbot.DeleteWebhookParams{}); err != nil {
		b.log.Warn("telegram: delete webhook", slog.Any("error", err))
	}
	if _, err := b.api.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{
		Commands: []models.BotCommand{{Command: "start", Description: "Начать"}},
	}); err != nil {
		b.log.Warn("telegram: set commands", slog.Any("error", err))
	}
	go b.api.Start(ctx)
	go b.reportLoop(ctx)
	b.eventLoop(ctx)
}

// OrderCreated queues a new-order notification.
func (b *Bot) OrderCreated(orderID int64) { b.enqueue(event{orderID: orderID, created: true}) }

// OrderUpdated queues a refresh of the order's messages.
func (b *Bot) OrderUpdated(orderID int64) { b.enqueue(event{orderID: orderID}) }

func (b *Bot) enqueue(e event) {
	select {
	case b.events <- e:
	default:
		b.log.Error("telegram: notification queue is full, dropping event", slog.Int64("order_id", e.orderID))
	}
}

func (b *Bot) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-b.events:
			var err error
			if e.created {
				err = b.notifyNewOrder(ctx, e.orderID)
			} else {
				err = b.refreshOrder(ctx, e.orderID)
			}
			if err != nil && ctx.Err() == nil {
				b.log.Error("telegram: order notification", slog.Int64("order_id", e.orderID), slog.Bool("created", e.created), slog.Any("error", err))
			}
		}
	}
}

// notifyNewOrder sends the order card to every active admin.
func (b *Bot) notifyNewOrder(ctx context.Context, orderID int64) error {
	o, err := b.admin.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}
	admins, err := b.store.ListActiveAdmins(ctx)
	if err != nil {
		return fmt.Errorf("list admins: %w", err)
	}
	text, keyboard := orderCard(o, b.cfg.Location), orderKeyboard(o.Order, b.adminURL)
	for _, a := range admins {
		var msg *models.Message
		err := b.retry(ctx, func() error {
			var err error
			msg, err = b.api.SendMessage(ctx, &tgbot.SendMessageParams{
				ChatID: a.TelegramID, Text: text, ParseMode: models.ParseModeHTML, ReplyMarkup: keyboard,
			})
			return err
		})
		if err != nil {
			// 403: the admin has not started the bot (or blocked it).
			b.log.Warn("telegram: send order", slog.Int64("order_id", orderID), slog.Int64("telegram_id", a.TelegramID), slog.Any("error", err))
			continue
		}
		if err := b.store.SaveOrderNotification(ctx, db.SaveOrderNotificationParams{
			OrderID: orderID, ChatID: a.TelegramID, MessageID: int64(msg.ID),
		}); err != nil {
			b.log.Error("telegram: save message ref", slog.Int64("order_id", orderID), slog.Any("error", err))
		}
	}
	return nil
}

// refreshOrder re-renders every message sent about the order.
func (b *Bot) refreshOrder(ctx context.Context, orderID int64) error {
	refs, err := b.store.ListOrderNotifications(ctx, orderID)
	if err != nil || len(refs) == 0 {
		return err
	}
	o, err := b.admin.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}
	text, keyboard := orderCard(o, b.cfg.Location), orderKeyboard(o.Order, b.adminURL)
	for _, ref := range refs {
		err := b.retry(ctx, func() error {
			_, err := b.api.EditMessageText(ctx, &tgbot.EditMessageTextParams{
				ChatID: ref.ChatID, MessageID: int(ref.MessageID), Text: text, ParseMode: models.ParseModeHTML, ReplyMarkup: keyboard,
			})
			return ignoreNotModified(err)
		})
		if err != nil {
			b.log.Warn("telegram: update order message", slog.Int64("order_id", orderID), slog.Int64("chat_id", ref.ChatID), slog.Any("error", err))
		}
	}
	return nil
}

func (b *Bot) handleStart(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	from := u.Message.From
	if from == nil {
		return
	}
	params := &tgbot.SendMessageParams{ChatID: u.Message.Chat.ID, ParseMode: models.ParseModeHTML}
	admin, err := b.store.GetActiveAdminByTelegramID(ctx, from.ID)
	switch {
	case err == nil:
		params.Text = fmt.Sprintf("Здравствуйте, %s! Сюда будут приходить новые заказы.", esc(admin.Name))
		if b.adminURL != "" {
			params.ReplyMarkup = &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{{Text: "Открыть админку", WebApp: &models.WebAppInfo{URL: b.adminURL}}},
			}}
		} else {
			params.Text += "\nКнопка админки появится, когда ADMIN_URL будет HTTPS."
		}
	case repo.IsNotFound(err):
		params.Text = fmt.Sprintf("Это бот магазина ZHIM. Каталог: %s\n\nЕсли вы сотрудник магазина, передайте владельцу ваш Telegram ID: <code>%d</code>",
			esc(b.cfg.WebURL), from.ID)
		if strings.HasPrefix(b.cfg.WebURL, "https://") {
			params.ReplyMarkup = &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{{Text: "Открыть магазин", URL: b.cfg.WebURL}},
			}}
		}
	default:
		b.log.Error("telegram: /start lookup", slog.Any("error", err))
		return
	}
	if _, err := b.api.SendMessage(ctx, params); err != nil {
		b.log.Warn("telegram: reply to /start", slog.Any("error", err))
	}
}

func (b *Bot) handleCallback(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	q := u.CallbackQuery
	answer := func(text string, alert bool) {
		if _, err := b.api.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID, Text: text, ShowAlert: alert}); err != nil {
			b.log.Warn("telegram: answer callback", slog.Any("error", err))
		}
	}

	admin, err := b.store.GetActiveAdminByTelegramID(ctx, q.From.ID)
	if err != nil {
		if !repo.IsNotFound(err) {
			b.log.Error("telegram: callback admin lookup", slog.Any("error", err))
		}
		answer("Нет доступа", true)
		return
	}
	c, ok := parseCallback(q.Data)
	if !ok {
		answer("Неизвестная команда", false)
		return
	}

	switch c.action {
	case actionStatus:
		_, err := b.admin.SetOrderStatus(ctx, &admin.ID, c.orderID, c.status)
		if se, ok := service.AsError(err); ok {
			answer(callbackError(se), true)
			b.OrderUpdated(c.orderID) // show the current state
			return
		}
		if err != nil {
			b.log.Error("telegram: set order status", slog.Int64("order_id", c.orderID), slog.Any("error", err))
			answer("Ошибка, попробуйте ещё раз", true)
			return
		}
		answer("Готово: "+statusLabels[c.status], false) // the service event refreshes all copies

	case actionCancel, actionBack:
		msg := q.Message.Message
		if msg == nil {
			answer("Сообщение устарело", false)
			return
		}
		keyboard := confirmCancelKeyboard(c.orderID)
		if c.action == actionBack {
			o, err := b.store.GetOrder(ctx, c.orderID)
			if err != nil {
				answer("Заказ не найден", true)
				return
			}
			keyboard = orderKeyboard(o, b.adminURL)
		}
		if _, err := b.api.EditMessageReplyMarkup(ctx, &tgbot.EditMessageReplyMarkupParams{
			ChatID: msg.Chat.ID, MessageID: msg.ID, ReplyMarkup: keyboard,
		}); ignoreNotModified(err) != nil {
			b.log.Warn("telegram: edit keyboard", slog.Any("error", err))
		}
		answer("", false)
	}
}

func callbackError(e *service.Error) string {
	switch e.Code {
	case service.CodeInvalidStatusTransition:
		return "Статус заказа уже изменён — карточка обновлена."
	case service.CodeNotFound:
		return "Заказ не найден."
	default:
		return "Не получилось: " + e.Message
	}
}

func (b *Bot) reportLoop(ctx context.Context) {
	for {
		at := nextRun(b.now(), b.cfg.ReportHour, b.cfg.ReportMinute, b.cfg.Location)
		timer := time.NewTimer(time.Until(at))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := b.SendDailyReport(ctx); err != nil {
				b.log.Error("telegram: daily report", slog.Any("error", err))
			}
		}
	}
}

// SendDailyReport sends today's summary to active owners (it includes revenue).
func (b *Bot) SendDailyReport(ctx context.Context) error {
	d, err := b.admin.Dashboard(ctx)
	if err != nil {
		return err
	}
	newStatus := db.OrderStatusNew
	awaiting, err := b.admin.ListOrders(ctx, service.OrderFilter{Status: &newStatus, PerPage: 1})
	if err != nil {
		return err
	}
	owners, err := b.store.ListActiveOwners(ctx)
	if err != nil {
		return fmt.Errorf("list owners: %w", err)
	}
	text := dailyReport(b.now().In(b.cfg.Location), d, awaiting.Total)
	for _, o := range owners {
		err := b.retry(ctx, func() error {
			_, err := b.api.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: o.TelegramID, Text: text, ParseMode: models.ParseModeHTML})
			return err
		})
		if err != nil {
			b.log.Warn("telegram: send daily report", slog.Int64("telegram_id", o.TelegramID), slog.Any("error", err))
		}
	}
	return nil
}

// retry runs fn until it succeeds, fails permanently or attempts run out.
// Rate limits wait for Telegram's retry_after; network and server errors back off.
func (b *Bot) retry(ctx context.Context, fn func() error) error {
	delay := b.retryDelay
	for attempt := 1; ; attempt++ {
		err := fn()
		if err == nil || attempt == b.attempts || !retryable(err) {
			return err
		}
		wait := delay
		var tooMany *tgbot.TooManyRequestsError
		if errors.As(err, &tooMany) && tooMany.RetryAfter > 0 {
			wait = time.Duration(tooMany.RetryAfter) * time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		delay *= 2
	}
}

func retryable(err error) bool {
	for _, permanent := range []error{tgbot.ErrorForbidden, tgbot.ErrorBadRequest, tgbot.ErrorUnauthorized, tgbot.ErrorNotFound, context.Canceled} {
		if errors.Is(err, permanent) {
			return false
		}
	}
	return true
}

// ignoreNotModified treats "message is not modified" (same text and keyboard) as success.
func ignoreNotModified(err error) error {
	if err != nil && errors.Is(err, tgbot.ErrorBadRequest) && strings.Contains(err.Error(), "message is not modified") {
		return nil
	}
	return err
}
