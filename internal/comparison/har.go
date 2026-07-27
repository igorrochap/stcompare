package comparison

import "sort"

const harVersion = "1.2"

type harDocument struct {
	Log harLog `json:"log"`
}

type harLog struct {
	Version string     `json:"version,omitempty"`
	Entries []harEntry `json:"entries"`
}

type harEntry struct {
	Request  harRequest   `json:"request,omitempty"`
	Response *harResponse `json:"response,omitempty"`
}

type harRequest struct {
	Method   string      `json:"method"`
	URL      string      `json:"url"`
	Headers  []harHeader `json:"headers"`
	PostData harPostData `json:"postData"`
}

type harHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harPostData struct {
	Text     string `json:"text"`
	Encoding string `json:"encoding"`
}

type harResponse struct {
	Status  int         `json:"status"`
	Headers []harHeader `json:"headers"`
	Content harContent  `json:"content"`
}

type harContent struct {
	Text string `json:"text"`
}

func sortedHARHeaders(headers []harHeader) []harHeader {
	sorted := append([]harHeader(nil), headers...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].Value < sorted[j].Value
	})

	return sorted
}

func flattenHeaderMap(headers map[string][]string) []harHeader {
	var flattened []harHeader
	for name, values := range headers {
		for _, value := range values {
			flattened = append(flattened, harHeader{Name: name, Value: value})
		}
	}

	return sortedHARHeaders(flattened)
}
