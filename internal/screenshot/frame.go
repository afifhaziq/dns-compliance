package screenshot

import (
	"context"
	"encoding/base64"
	"html"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// malaysiaTime is a fixed UTC+8 zone (Malaysia has no DST) so the embedded
// screenshot timestamp doesn't depend on a system tzdata database being
// installed in the runtime container.
var malaysiaTime = time.FixedZone("MYT", 8*60*60)

const browserHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif;
  background: #dcdcdc;
  width: 1920px;
  overflow: hidden;
}
.mac-topbar {
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 10px 14px 0;
  height: 38px;
}
.traffic-lights {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}
.tl {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  box-shadow: inset 0 0 0 0.5px rgba(0,0,0,0.15);
}
.tl-close { background: #ff5f57; }
.tl-min { background: #febc2e; }
.tl-max { background: #28c840; }
.tab-strip {
  display: flex;
  align-items: flex-end;
  flex: 1;
  height: 100%;
}
.tab {
  display: flex;
  align-items: center;
  background: white;
  border-radius: 8px 8px 0 0;
  padding: 0 14px;
  height: 30px;
  min-width: 180px;
  max-width: 240px;
  gap: 8px;
}
.favicon {
  width: 14px;
  height: 14px;
  border-radius: 2px;
  background: #8a8a8e;
  flex-shrink: 0;
}
.tab-title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: #3c3c43;
}
.tab-close { color: #8a8a8e; font-size: 14px; }
.toolbar {
  background: white;
  display: flex;
  align-items: center;
  padding: 8px 12px;
  gap: 6px;
  height: 46px;
  border-bottom: 1px solid #d9d9d9;
}
.nav-btn {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #8a8a8e;
  font-size: 15px;
  flex-shrink: 0;
}
.address-bar {
  flex: 1;
  background: #f0f0f0;
  border-radius: 14px;
  height: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 14px;
  gap: 6px;
  margin: 0 8px;
}
.lock { color: #3c3c43; flex-shrink: 0; }
.url-text {
  font-size: 13px;
  color: #3c3c43;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.menu-btn { color: #8a8a8e; font-size: 18px; flex-shrink: 0; }
.clock-chip {
  font-family: 'Menlo', 'SF Mono', 'Consolas', monospace;
  font-size: 12px;
  color: #3c3c43;
  background: #f0f0f0;
  padding: 4px 10px;
  border-radius: 6px;
  white-space: nowrap;
  flex-shrink: 0;
}
.page { line-height: 0; }
.page img { width: 1920px; display: block; }
</style>
</head>
<body>
<div class="mac-topbar">
  <div class="traffic-lights">
    <span class="tl tl-close"></span>
    <span class="tl tl-min"></span>
    <span class="tl tl-max"></span>
  </div>
  <div class="tab-strip">
    <div class="tab">
      <div class="favicon"></div>
      <span class="tab-title">{{HOSTNAME}}</span>
      <span class="tab-close">&#215;</span>
    </div>
  </div>
</div>
<div class="toolbar">
  <div class="nav-btn">&#8592;</div>
  <div class="nav-btn" style="opacity:.35">&#8594;</div>
  <div class="nav-btn">&#8635;</div>
  <div class="address-bar">
    <svg class="lock" width="12" height="12" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect x="5" y="11" width="14" height="10" rx="2" stroke="currentColor" stroke-width="2"/>
      <path d="M8 11V7a4 4 0 0 1 8 0v4" stroke="currentColor" stroke-width="2"/>
    </svg>
    <span class="url-text">{{URL}}</span>
  </div>
  <div class="clock-chip">{{TIMESTAMP}}</div>
  {{ISP_CHIP}}
  <div class="menu-btn">&#8942;</div>
</div>
<div class="page">
  <img src="data:image/png;base64,{{BASE64}}" />
</div>
</body>
</html>`

// ispDNSLabel joins isp and dnsAddress with " · ", falling back to whichever
// single value is set, or "" when both are empty (system resolver — no
// second chip is rendered in that case).
func ispDNSLabel(isp, dnsAddress string) string {
	switch {
	case isp != "" && dnsAddress != "":
		return isp + " · " + dnsAddress
	case isp != "":
		return isp
	default:
		return dnsAddress
	}
}

// buildFrameHTML renders the browser-mockup HTML with the page screenshot,
// timestamp chip, and (when isp/dnsAddress carry a value) a second chip
// identifying which ISP/DNS server resolved this capture. Pure string
// templating — no chromedp — so it's unit-testable without Chrome.
func buildFrameHTML(pageBytes []byte, rawURL string, capturedAt time.Time, isp, dnsAddress string) string {
	hostname := hostnameFromURL(rawURL)
	ispChip := ""
	if label := ispDNSLabel(isp, dnsAddress); label != "" {
		ispChip = `<div class="clock-chip">` + html.EscapeString(label) + `</div>`
	}
	return strings.NewReplacer(
		"{{HOSTNAME}}", html.EscapeString(hostname),
		"{{URL}}", html.EscapeString(rawURL),
		"{{TIMESTAMP}}", html.EscapeString(capturedAt.In(malaysiaTime).Format("2006-01-02 15:04:05 MST")),
		"{{ISP_CHIP}}", ispChip,
		"{{BASE64}}", base64.StdEncoding.EncodeToString(pageBytes),
	).Replace(browserHTML)
}

// Frame composites pageBytes into a Chrome-like browser mockup by navigating
// to a locally generated HTML page and screenshotting it. capturedAt is
// stamped into the mockup as a system-tray-style clock chip. isp/dnsAddress,
// when non-empty, render a second chip identifying which ISP/DNS server
// produced this capture — pass "" for both when there's no DNS server
// context (system resolver). chromeCtx is a chromedp tab context; callers
// may reuse the same one across multiple Frame calls since this only
// navigates to a local data: URL, never the target site.
func Frame(chromeCtx context.Context, pageBytes []byte, rawURL string, capturedAt time.Time, isp, dnsAddress string) ([]byte, error) {
	htmlContent := buildFrameHTML(pageBytes, rawURL, capturedAt, isp, dnsAddress)
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

// addBrowserFrame is the old unexported function, kept for backward compatibility
// until Task 2 updates callers to use Frame instead. Calls Frame with empty
// ISP and DNS address.
func addBrowserFrame(chromeCtx context.Context, pageBytes []byte, rawURL string, capturedAt time.Time) ([]byte, error) {
	return Frame(chromeCtx, pageBytes, rawURL, capturedAt, "", "")
}

func hostnameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return rawURL
}
