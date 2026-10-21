package domain

// SizeChartData is the JSONB payload of size_charts.data.
type SizeChartData struct {
	Unit    string            `json:"unit"`
	Columns []SizeChartColumn `json:"columns"`
	Rows    []SizeChartRow    `json:"rows"`
	Fit     []SizeFitRule     `json:"fit"`
}

// SizeChartColumn is one measurement, e.g. chest girth.
type SizeChartColumn struct {
	Key    string `json:"key"`
	NameRu string `json:"name_ru"`
	NameKy string `json:"name_ky"`
}

// SizeChartRow holds measurements for one size, keyed by SizeChartColumn.Key.
type SizeChartRow struct {
	Size   string            `json:"size"`
	Values map[string]string `json:"values"`
}

// SizeFitRule recommends a size by height (cm) and weight (kg).
type SizeFitRule struct {
	Size      string `json:"size"`
	HeightMin int    `json:"height_min"`
	HeightMax int    `json:"height_max"`
	WeightMin int    `json:"weight_min"`
	WeightMax int    `json:"weight_max"`
}
