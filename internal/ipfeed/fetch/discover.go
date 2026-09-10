package fetch

import (
	"context"
	"fmt"
	"regexp"
)

// azureJSONRe matches the current dated Service Tags JSON link on the Azure
// download page. Microsoft publishes the file weekly with a date in the name,
// so the URL must be discovered rather than hard-coded.
var azureJSONRe = regexp.MustCompile(`https://download\.microsoft\.com/download/[^"']*ServiceTags_Public_\d+\.json`)

// Discover resolves a possibly-dynamic feed URL to the concrete URL to fetch.
// mode "" or "none" returns pageURL unchanged. "azure_download_page" fetches
// the download page and extracts the current dated JSON link.
func (c *Client) Discover(ctx context.Context, mode, pageURL string) (string, error) {
	switch mode {
	case "", "none":
		return pageURL, nil
	case "azure_download_page":
		res, err := c.Get(ctx, pageURL, Conditional{})
		if err != nil {
			return "", err
		}
		if m := azureJSONRe.Find(res.Body); m != nil {
			return string(m), nil
		}
		return "", fmt.Errorf("azure discovery: no ServiceTags JSON link found on %s", pageURL)
	default:
		return "", fmt.Errorf("unknown discover mode %q", mode)
	}
}
