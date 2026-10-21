package seed

import "github.com/Baiel1922/zhim-back/backend/internal/domain"

// TODO(i18n): Kyrgyz texts below need proofreading by a native speaker.

type category struct {
	slug, nameRu, nameKy string
}

var categories = []category{
	{"termofutbolki", "Термофутболки", "Термофутболкалар"},
	{"longslivy", "Лонгсливы", "Лонгсливдер"},
	{"shorty", "Шорты", "Шорттор"},
}

type color struct {
	code, nameRu, nameKy, hex string
}

var colors = []color{
	{"BLK", "Чёрный", "Кара", "#0A0A0A"},
	{"WHT", "Белый", "Ак", "#F5F5F5"},
	{"GRF", "Графит", "Графит", "#3A3A3C"},
}

var sizes = []string{"S", "M", "L", "XL", "XXL"}

// baseStock is the per-size stock (same order as sizes) before overrides.
var baseStock = []int32{5, 9, 12, 7, 3}

type product struct {
	code, slug, category string
	titleRu, titleKy     string
	descriptionRu        string
	descriptionKy        string
	price                int32
	oldPrice             *int32
	isNew                bool
	colors               [2]string        // color codes
	stockOverrides       map[string]int32 // "<color>-<size>" -> stock
}

const (
	compositionRu = "88% полиэстер, 12% эластан"
	compositionKy = "88% полиэстер, 12% эластан"
	careRu        = "Стирка при 30 °C, не отбеливать, не гладить, сушить в расправленном виде."
	careKy        = "30 °C жууңуз, агартпаңыз, үтүктөбөңүз, жайып кургатыңыз."
)

func price(v int32) *int32 { return &v }

var products = []product{
	{
		code: "TFC", slug: "termofutbolka-core", category: "termofutbolki",
		titleRu: "Термофутболка Core", titleKy: "Core термофутболкасы",
		descriptionRu: "Базовая термофутболка из быстросохнущей ткани. Отводит влагу, не сковывает движений и держит форму после стирок.",
		descriptionKy: "Тез кургаган кездемеден жасалган негизги термофутболка. Нымды сыртка чыгарат, кыймылга тоскоол болбойт жана жуулгандан кийин формасын сактайт.",
		price:         1990, isNew: true,
		colors: [2]string{"BLK", "WHT"},
	},
	{
		code: "TFP", slug: "termofutbolka-pro-compression", category: "termofutbolki",
		titleRu: "Термофутболка Pro Compression", titleKy: "Pro Compression термофутболкасы",
		descriptionRu: "Компрессионная посадка для силовых и интервальных тренировок. Плоские швы не натирают.",
		descriptionKy: "Күч жана интервалдык машыгуулар үчүн компрессиялык тигилиш. Жалпак тигиштер сүрткүлөбөйт.",
		price:         2490, oldPrice: price(2990),
		colors:         [2]string{"BLK", "GRF"},
		stockOverrides: map[string]int32{"GRF-XXL": 0, "BLK-S": 1},
	},
	{
		code: "LST", slug: "longsliv-thermo", category: "longslivy",
		titleRu: "Лонгслив Thermo", titleKy: "Thermo лонгсливи",
		descriptionRu: "Тёплый лонгслив с начёсом для тренировок на улице в холодную погоду.",
		descriptionKy: "Суук күндөрү сыртта машыгуу үчүн ичи түктүү жылуу лонгслив.",
		price:         2790, isNew: true,
		colors:         [2]string{"BLK", "GRF"},
		stockOverrides: map[string]int32{"BLK-XXL": 1},
	},
	{
		code: "LSR", slug: "longsliv-run", category: "longslivy",
		titleRu: "Лонгслив Run", titleKy: "Run лонгсливи",
		descriptionRu: "Лёгкий лонгслив для бега со светоотражающими элементами и вырезом для большого пальца.",
		descriptionKy: "Жарык чагылдыруучу элементтери жана бармак үчүн тешиги бар чуркоого арналган жеңил лонгслив.",
		price:         2590,
		colors:        [2]string{"BLK", "WHT"},
	},
	{
		code: "SHT", slug: "shorty-training", category: "shorty",
		titleRu: "Шорты Training", titleKy: "Training шорту",
		descriptionRu:  "Тренировочные шорты со встроенными тайтсами и карманом на молнии.",
		descriptionKy:  "Ичине тайтсы орнотулган жана сыдырмалуу чөнтөгү бар машыгуу шорту.",
		price:          2290,
		colors:         [2]string{"BLK", "GRF"},
		stockOverrides: map[string]int32{"GRF-S": 0, "GRF-M": 0},
	},
	{
		code: "SHR", slug: "shorty-run", category: "shorty",
		titleRu: "Шорты Run", titleKy: "Run шорту",
		descriptionRu: "Сверхлёгкие беговые шорты длиной 5 дюймов с сетчатой подкладкой.",
		descriptionKy: "Ичи тор кездемелүү, узундугу 5 дюйм болгон өтө жеңил чуркоо шорту.",
		price:         1890, oldPrice: price(2290),
		colors:         [2]string{"BLK", "WHT"},
		stockOverrides: map[string]int32{"WHT-L": 1},
	},
}

const sizeChartName = "Мужская одежда"

var sizeChart = domain.SizeChartData{
	Unit: "cm",
	Columns: []domain.SizeChartColumn{
		{Key: "chest", NameRu: "Обхват груди", NameKy: "Көкүрөк айланасы"},
		{Key: "waist", NameRu: "Обхват талии", NameKy: "Бел айланасы"},
		{Key: "hips", NameRu: "Обхват бёдер", NameKy: "Жамбаш айланасы"},
	},
	Rows: []domain.SizeChartRow{
		{Size: "S", Values: map[string]string{"chest": "88–94", "waist": "74–80", "hips": "90–96"}},
		{Size: "M", Values: map[string]string{"chest": "94–100", "waist": "80–86", "hips": "96–102"}},
		{Size: "L", Values: map[string]string{"chest": "100–106", "waist": "86–92", "hips": "102–108"}},
		{Size: "XL", Values: map[string]string{"chest": "106–112", "waist": "92–98", "hips": "108–114"}},
		{Size: "XXL", Values: map[string]string{"chest": "112–118", "waist": "98–104", "hips": "114–120"}},
	},
	Fit: []domain.SizeFitRule{
		{Size: "S", HeightMin: 165, HeightMax: 175, WeightMin: 55, WeightMax: 67},
		{Size: "M", HeightMin: 170, HeightMax: 180, WeightMin: 65, WeightMax: 77},
		{Size: "L", HeightMin: 175, HeightMax: 185, WeightMin: 75, WeightMax: 87},
		{Size: "XL", HeightMin: 180, HeightMax: 190, WeightMin: 85, WeightMax: 97},
		{Size: "XXL", HeightMin: 185, HeightMax: 195, WeightMin: 95, WeightMax: 110},
	},
}

type deliveryZone struct {
	nameRu, nameKy  string
	price, freeFrom int32
}

var deliveryZones = []deliveryZone{
	{"Бишкек", "Бишкек", 200, 5000},
	{"Пригород Бишкека", "Бишкектин айланасы", 350, 8000},
}

// Placeholder contacts: replace in the admin panel.
var (
	contacts = domain.Contacts{
		Phones:    []string{"+996700000000"},
		WhatsApp:  "+996700000000",
		Telegram:  "zhim_kg",
		Instagram: "zhim.kg",
	}
	pickup = domain.Pickup{
		AddressRu: "г. Бишкек, ул. Примерная, 1",
		AddressKy: "Бишкек ш., Примерная көч., 1",
		HoursRu:   "Пн–Сб 10:00–19:00",
		HoursKy:   "Дш–Иш 10:00–19:00",
	}
	hero = domain.Hero{
		TitleRu:    "Одежда для тех, кто тренируется",
		TitleKy:    "Машыгып жүргөндөр үчүн кийим",
		SubtitleRu: "Термофутболки, лонгсливы и шорты. Доставка по Бишкеку.",
		SubtitleKy: "Термофутболкалар, лонгсливдер жана шорттор. Бишкек боюнча жеткирүү.",
	}
)
