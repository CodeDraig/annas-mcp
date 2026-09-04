package anna

import "time"

const (
	AnnasSearchEndpointFormat   = "https://%s/search?q=%s&content=%s"
	AnnasDownloadEndpointFormat = "https://%s/dyn/api/fast_download.json?md5=%s&key=%s"
	DefaultHTTPTimeout          = time.Hour
	BrowserUserAgent            = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)
