package screenshot

import (
	"context"
	"encoding/base64"
	"html"
	"math"
	"net/url"
	"strings"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const browserHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
  font-family: 'Segoe UI', Arial, sans-serif;
  background: #dee1e6;
  width: 1920px;
  overflow: hidden;
}
.tab-strip {
  display: flex;
  align-items: flex-end;
  padding: 8px 8px 0;
  height: 44px;
}
.tab {
  display: flex;
  align-items: center;
  background: white;
  border-radius: 8px 8px 0 0;
  padding: 0 14px;
  height: 36px;
  min-width: 180px;
  max-width: 240px;
  gap: 8px;
  box-shadow: 0 1px 3px rgba(0,0,0,0.15);
}
.favicon {
  width: 16px;
  height: 16px;
  border-radius: 2px;
  background: #5f6368;
  flex-shrink: 0;
}
.tab-title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  color: #3c4043;
}
.tab-close { color: #5f6368; font-size: 15px; }
.toolbar {
  background: #f1f3f4;
  display: flex;
  align-items: center;
  padding: 6px 8px;
  gap: 4px;
  height: 48px;
  border-bottom: 1px solid #dadce0;
}
.nav-btn {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #5f6368;
  font-size: 16px;
  flex-shrink: 0;
}
.address-bar {
  flex: 1;
  background: white;
  border-radius: 20px;
  height: 32px;
  display: flex;
  align-items: center;
  padding: 0 14px;
  gap: 6px;
  margin: 0 8px;
  box-shadow: 0 1px 2px rgba(0,0,0,0.1);
}
.lock { color: #137333; font-size: 13px; font-weight: 500; }
.url-text {
  font-size: 14px;
  color: #202124;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.menu-btn { color: #5f6368; font-size: 20px; flex-shrink: 0; }
.page { line-height: 0; }
.page img { width: 1920px; display: block; }
</style>
</head>
<body>
<div class="tab-strip">
  <div class="tab">
    <div class="favicon"></div>
    <span class="tab-title">{{HOSTNAME}}</span>
    <span class="tab-close">&#215;</span>
  </div>
</div>
<div class="toolbar">
  <div class="nav-btn">&#8592;</div>
  <div class="nav-btn" style="opacity:.35">&#8594;</div>
  <div class="nav-btn">&#8635;</div>
  <div class="address-bar">
    <span class="lock">&#128274;</span>
    <span class="url-text">{{URL}}</span>
  </div>
  <div class="menu-btn">&#8942;</div>
</div>
<div class="page">
  <img src="data:image/png;base64,{{BASE64}}" />
</div>
</body>
</html>`

// addBrowserFrame composites pageBytes into a Chrome-like browser mockup by
// navigating to a locally generated HTML page and screenshotting it.
func addBrowserFrame(chromeCtx context.Context, pageBytes []byte, rawURL string) ([]byte, error) {
	hostname := hostnameFromURL(rawURL)
	htmlContent := strings.NewReplacer(
		"{{HOSTNAME}}", html.EscapeString(hostname),
		"{{URL}}", html.EscapeString(rawURL),
		"{{BASE64}}", base64.StdEncoding.EncodeToString(pageBytes),
	).Replace(browserHTML)

	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(htmlContent))

	var buf []byte
	if err := chromedp.Run(chromeCtx,
		chromedp.Navigate(dataURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, contentSize, _, _, _, err := page.GetLayoutMetrics().Do(ctx)
			if err != nil {
				return err
			}
			fullH := int64(math.Ceil(contentSize.Height))
			if err := emulation.SetDeviceMetricsOverride(1920, fullH, 1, false).Do(ctx); err != nil {
				return err
			}
			buf, err = page.CaptureScreenshot().
				WithFormat(page.CaptureScreenshotFormatPng).
				WithCaptureBeyondViewport(true).
				Do(ctx)
			return err
		}),
	); err != nil {
		return nil, err
	}
	return buf, nil
}

func hostnameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return rawURL
}
