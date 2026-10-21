package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Baiel1922/zhim-back/backend/internal/domain"
	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/revalidate"
)

var (
	promoCodeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{3,32}$`)
	telegramRe  = regexp.MustCompile(`^[A-Za-z0-9_]{5,32}$`)
	instagramRe = regexp.MustCompile(`^[A-Za-z0-9._]{1,30}$`)
)

const maxSettingLen = 300

// ---------- Delivery zones ----------

// DeliveryZoneInput is a delivery zone form.
type DeliveryZoneInput struct {
	NameRu, NameKy string
	Price          int32
	FreeFrom       *int32
	IsActive       bool
	Sort           int32
}

func (in DeliveryZoneInput) normalize() (DeliveryZoneInput, error) {
	var f fields
	in.NameRu = f.text("name_ru", in.NameRu, maxShortNameLen)
	in.NameKy = f.text("name_ky", in.NameKy, maxShortNameLen)
	if in.Price < 0 {
		f.add("price", FieldInvalid)
	}
	if in.FreeFrom != nil && *in.FreeFrom <= 0 {
		f.add("free_from", FieldInvalid)
	}
	return in, f.err()
}

// ListDeliveryZones returns all zones, inactive included.
func (a *Admin) ListDeliveryZones(ctx context.Context) ([]db.DeliveryZone, error) {
	return a.store.AdminListDeliveryZones(ctx)
}

// CreateDeliveryZone adds a zone.
func (a *Admin) CreateDeliveryZone(ctx context.Context, in DeliveryZoneInput) (db.DeliveryZone, error) {
	in, err := in.normalize()
	if err != nil {
		return db.DeliveryZone{}, err
	}
	z, err := a.store.CreateDeliveryZone(ctx, db.CreateDeliveryZoneParams{
		NameRu: in.NameRu, NameKy: in.NameKy, Price: in.Price, FreeFrom: in.FreeFrom, IsActive: in.IsActive, Sort: in.Sort,
	})
	if repo.IsUniqueViolation(err, "delivery_zones_name_ru_key") {
		return db.DeliveryZone{}, taken("name_ru")
	}
	if err != nil {
		return db.DeliveryZone{}, fmt.Errorf("create delivery zone: %w", err)
	}
	a.notify.Notify(revalidate.TagSettings)
	return z, nil
}

// UpdateDeliveryZone replaces a zone.
func (a *Admin) UpdateDeliveryZone(ctx context.Context, id int64, in DeliveryZoneInput) (db.DeliveryZone, error) {
	in, err := in.normalize()
	if err != nil {
		return db.DeliveryZone{}, err
	}
	z, err := a.store.UpdateDeliveryZone(ctx, db.UpdateDeliveryZoneParams{
		ID: id, NameRu: in.NameRu, NameKy: in.NameKy, Price: in.Price, FreeFrom: in.FreeFrom, IsActive: in.IsActive, Sort: in.Sort,
	})
	switch {
	case repo.IsUniqueViolation(err, "delivery_zones_name_ru_key"):
		return db.DeliveryZone{}, taken("name_ru")
	case err != nil:
		return db.DeliveryZone{}, lookupErr(err, "delivery zone")
	}
	a.notify.Notify(revalidate.TagSettings)
	return z, nil
}

// DeleteDeliveryZone removes a zone no order references.
func (a *Admin) DeleteDeliveryZone(ctx context.Context, id int64) error {
	n, err := a.store.DeleteDeliveryZone(ctx, id)
	if repo.IsForeignKeyViolation(err, "orders_delivery_zone_id_fkey") {
		return newError(CodeInUse, "delivery zone is used by orders; deactivate it instead")
	}
	if err := deleted(n, err, "delivery zone"); err != nil {
		return err
	}
	a.notify.Notify(revalidate.TagSettings)
	return nil
}

// ---------- Promo codes ----------

// PromoCodeInput is a promo code form.
type PromoCodeInput struct {
	Code               string
	Type               db.PromoType
	Value, MinTotal    int32
	MaxUses            *int32
	ValidFrom, ValidTo *time.Time
	IsActive           bool
}

func (in PromoCodeInput) normalize() (PromoCodeInput, error) {
	var f fields
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	if !promoCodeRe.MatchString(in.Code) {
		f.add("code", FieldInvalid)
	}
	if !in.Type.Valid() {
		f.add("type", FieldInvalid)
	}
	if in.Value <= 0 || (in.Type == db.PromoTypePercent && in.Value > 100) {
		f.add("value", FieldInvalid)
	}
	if in.MinTotal < 0 {
		f.add("min_total", FieldInvalid)
	}
	if in.MaxUses != nil && *in.MaxUses <= 0 {
		f.add("max_uses", FieldInvalid)
	}
	if in.ValidFrom != nil && in.ValidTo != nil && !in.ValidTo.After(*in.ValidFrom) {
		f.add("valid_to", FieldInvalid)
	}
	return in, f.err()
}

func promoWriteErr(err error) error {
	switch {
	case repo.IsUniqueViolation(err, "promo_codes_code_key"):
		return taken("code")
	case repo.IsCheckViolation(err, "promo_codes_max_uses_chk"):
		return &Error{Code: CodeValidation, Message: "max_uses is below the uses so far", Fields: []FieldError{{"max_uses", FieldInvalid}}}
	}
	return err
}

// ListPromoCodes returns all promo codes, newest first.
func (a *Admin) ListPromoCodes(ctx context.Context) ([]db.PromoCode, error) {
	return a.store.AdminListPromoCodes(ctx)
}

// CreatePromoCode adds a promo code (stored upper-case; matching ignores case).
func (a *Admin) CreatePromoCode(ctx context.Context, in PromoCodeInput) (db.PromoCode, error) {
	in, err := in.normalize()
	if err != nil {
		return db.PromoCode{}, err
	}
	p, err := a.store.CreatePromoCode(ctx, db.CreatePromoCodeParams{
		Code: in.Code, Type: in.Type, Value: in.Value, MinTotal: in.MinTotal, MaxUses: in.MaxUses,
		ValidFrom: in.ValidFrom, ValidTo: in.ValidTo, IsActive: in.IsActive,
	})
	if err != nil {
		return db.PromoCode{}, fmt.Errorf("create promo code: %w", promoWriteErr(err))
	}
	return p, nil
}

// UpdatePromoCode replaces a promo code; used_count is kept.
func (a *Admin) UpdatePromoCode(ctx context.Context, id int64, in PromoCodeInput) (db.PromoCode, error) {
	in, err := in.normalize()
	if err != nil {
		return db.PromoCode{}, err
	}
	p, err := a.store.UpdatePromoCode(ctx, db.UpdatePromoCodeParams{
		ID: id, Code: in.Code, Type: in.Type, Value: in.Value, MinTotal: in.MinTotal, MaxUses: in.MaxUses,
		ValidFrom: in.ValidFrom, ValidTo: in.ValidTo, IsActive: in.IsActive,
	})
	if err != nil {
		if repo.IsNotFound(err) {
			return db.PromoCode{}, notFound("promo code")
		}
		return db.PromoCode{}, fmt.Errorf("update promo code: %w", promoWriteErr(err))
	}
	return p, nil
}

// DeletePromoCode removes a promo code; orders keep their discount.
func (a *Admin) DeletePromoCode(ctx context.Context, id int64) error {
	n, err := a.store.DeletePromoCode(ctx, id)
	return deleted(n, err, "promo code")
}

// ---------- Settings ----------

// StoreSettings are the editable store settings.
type StoreSettings struct {
	Contacts domain.Contacts
	Pickup   domain.Pickup
	Hero     domain.Hero
}

// GetSettings returns current settings (zero values for missing keys).
func (a *Admin) GetSettings(ctx context.Context) (*StoreSettings, error) {
	s, err := NewCatalog(a.store).PublicSettings(ctx)
	if err != nil {
		return nil, err
	}
	return &StoreSettings{Contacts: s.Contacts, Pickup: s.Pickup, Hero: s.Hero}, nil
}

// UpdateSettings validates and replaces all settings.
func (a *Admin) UpdateSettings(ctx context.Context, in StoreSettings) (*StoreSettings, error) {
	var f fields
	c := &in.Contacts
	phones := make([]string, 0, len(c.Phones))
	for i, p := range c.Phones {
		normalized, ok := NormalizePhone(p)
		if !ok {
			f.add(fmt.Sprintf("contacts.phones[%d]", i), FieldInvalid)
			continue
		}
		phones = append(phones, normalized)
	}
	if len(c.Phones) > 5 {
		f.add("contacts.phones", FieldTooMany)
	}
	c.Phones = phones
	if c.WhatsApp = strings.TrimSpace(c.WhatsApp); c.WhatsApp != "" {
		if normalized, ok := NormalizePhone(c.WhatsApp); ok {
			c.WhatsApp = normalized
		} else {
			f.add("contacts.whatsapp", FieldInvalid)
		}
	}
	if c.Telegram = strings.TrimPrefix(strings.TrimSpace(c.Telegram), "@"); c.Telegram != "" && !telegramRe.MatchString(c.Telegram) {
		f.add("contacts.telegram", FieldInvalid)
	}
	if c.Instagram = strings.TrimPrefix(strings.TrimSpace(c.Instagram), "@"); c.Instagram != "" && !instagramRe.MatchString(c.Instagram) {
		f.add("contacts.instagram", FieldInvalid)
	}

	p := &in.Pickup
	p.AddressRu = f.optionalText("pickup.address_ru", p.AddressRu, maxSettingLen)
	p.AddressKy = f.optionalText("pickup.address_ky", p.AddressKy, maxSettingLen)
	p.HoursRu = f.optionalText("pickup.hours_ru", p.HoursRu, maxSettingLen)
	p.HoursKy = f.optionalText("pickup.hours_ky", p.HoursKy, maxSettingLen)
	if p.MapURL = strings.TrimSpace(p.MapURL); p.MapURL != "" {
		if u, err := url.Parse(p.MapURL); err != nil || u.Scheme != "https" || u.Host == "" {
			f.add("pickup.map_url", FieldInvalid)
		}
	}

	h := &in.Hero
	h.TitleRu = f.optionalText("hero.title_ru", h.TitleRu, maxSettingLen)
	h.TitleKy = f.optionalText("hero.title_ky", h.TitleKy, maxSettingLen)
	h.SubtitleRu = f.optionalText("hero.subtitle_ru", h.SubtitleRu, maxSettingLen)
	h.SubtitleKy = f.optionalText("hero.subtitle_ky", h.SubtitleKy, maxSettingLen)
	if err := f.err(); err != nil {
		return nil, err
	}

	values := map[string]any{domain.SettingContacts: in.Contacts, domain.SettingPickup: in.Pickup, domain.SettingHero: in.Hero}
	err := a.store.InTx(ctx, func(q *db.Queries) error {
		for key, v := range values {
			raw, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("encode setting %s: %w", key, err)
			}
			if _, err := q.UpsertSetting(ctx, db.UpsertSettingParams{Key: key, Value: raw}); err != nil {
				return fmt.Errorf("save setting %s: %w", key, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	a.notify.Notify(revalidate.TagSettings)
	return &in, nil
}

// ---------- Admins ----------

// AdminInput is an admin account form.
type AdminInput struct {
	TelegramID int64 // used on create only
	Name       string
	Role       db.AdminRole
	IsActive   bool
}

func (in AdminInput) normalize(creating bool) (AdminInput, error) {
	var f fields
	if creating && in.TelegramID <= 0 {
		f.add("telegram_id", FieldInvalid)
	}
	in.Name = f.text("name", in.Name, maxShortNameLen)
	if !in.Role.Valid() {
		f.add("role", FieldInvalid)
	}
	return in, f.err()
}

// ListAdmins returns all admin accounts.
func (a *Admin) ListAdmins(ctx context.Context) ([]db.Admin, error) {
	return a.store.ListAdmins(ctx)
}

// CreateAdmin grants admin access to a Telegram user.
func (a *Admin) CreateAdmin(ctx context.Context, in AdminInput) (db.Admin, error) {
	in, err := in.normalize(true)
	if err != nil {
		return db.Admin{}, err
	}
	admin, err := a.store.CreateAdmin(ctx, db.CreateAdminParams{TelegramID: in.TelegramID, Name: in.Name, Role: in.Role, IsActive: in.IsActive})
	if repo.IsUniqueViolation(err, "admins_telegram_id_key") {
		return db.Admin{}, taken("telegram_id")
	}
	if err != nil {
		return db.Admin{}, fmt.Errorf("create admin: %w", err)
	}
	return admin, nil
}

// UpdateAdmin changes name, role and access. The last active owner cannot be
// demoted or deactivated, so the store always has someone who can manage it.
func (a *Admin) UpdateAdmin(ctx context.Context, id int64, in AdminInput) (db.Admin, error) {
	in, err := in.normalize(false)
	if err != nil {
		return db.Admin{}, err
	}
	var updated db.Admin
	err = a.store.InTx(ctx, func(q *db.Queries) error {
		losesOwner := in.Role != db.AdminRoleOwner || !in.IsActive
		if err := ensureOtherOwner(ctx, q, id, losesOwner); err != nil {
			return err
		}
		updated, err = q.UpdateAdmin(ctx, db.UpdateAdminParams{ID: id, Name: in.Name, Role: in.Role, IsActive: in.IsActive})
		if err != nil {
			return lookupErr(err, "admin")
		}
		return nil
	})
	return updated, err
}

// DeleteAdmin removes an admin; the last active owner cannot be removed.
func (a *Admin) DeleteAdmin(ctx context.Context, id int64) error {
	return a.store.InTx(ctx, func(q *db.Queries) error {
		if err := ensureOtherOwner(ctx, q, id, true); err != nil {
			return err
		}
		n, err := q.DeleteAdmin(ctx, id)
		return deleted(n, err, "admin")
	})
}

// ensureOtherOwner fails with last_owner when id is the only active owner and
// the change would take that away. It locks owner rows to serialize such changes.
func ensureOtherOwner(ctx context.Context, q *db.Queries, id int64, losesOwner bool) error {
	owners, err := q.LockActiveOwners(ctx)
	if err != nil {
		return fmt.Errorf("lock owners: %w", err)
	}
	isOwner := false
	for _, o := range owners {
		isOwner = isOwner || o == id
	}
	if losesOwner && isOwner && len(owners) == 1 {
		return newError(CodeLastOwner, "the store must keep at least one active owner")
	}
	return nil
}
