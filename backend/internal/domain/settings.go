// Package domain holds business types shared across layers, including the
// shapes of JSONB columns (settings values, size chart data).
package domain

// Keys of the settings table.
const (
	SettingContacts = "contacts"
	SettingPickup   = "pickup"
	SettingHero     = "hero"
)

// Contacts is the value of the "contacts" setting.
type Contacts struct {
	Phones    []string `json:"phones"`
	WhatsApp  string   `json:"whatsapp"`
	Telegram  string   `json:"telegram"`
	Instagram string   `json:"instagram"`
}

// Pickup is the value of the "pickup" setting: the self-pickup point.
type Pickup struct {
	AddressRu string `json:"address_ru"`
	AddressKy string `json:"address_ky"`
	HoursRu   string `json:"hours_ru"`
	HoursKy   string `json:"hours_ky"`
	MapURL    string `json:"map_url,omitempty"`
}

// Hero is the value of the "hero" setting: home page hero texts.
type Hero struct {
	TitleRu    string `json:"title_ru"`
	TitleKy    string `json:"title_ky"`
	SubtitleRu string `json:"subtitle_ru"`
	SubtitleKy string `json:"subtitle_ky"`
}
