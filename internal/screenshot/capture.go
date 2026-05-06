package screenshot

import (
	"context"
	"math"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Capture launches a headless Chrome instance, navigates to rawURL, and returns
// a full-page PNG screenshot. The context controls the total time budget;
// cancelling it will abort the browser process.
func Capture(ctx context.Context, rawURL string) ([]byte, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	chromeCtx, chromeCancel := chromedp.NewContext(allocCtx)
	defer chromeCancel()

	var buf []byte
	if err := chromedp.Run(chromeCtx,
		// Render at a standard desktop width before navigating
		chromedp.EmulateViewport(1920, 1080),
		chromedp.Navigate(rawURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Measure the full page height after load
			_, _, contentSize, _, _, _, err := page.GetLayoutMetrics().Do(ctx)
			if err != nil {
				return err
			}
			// Expand the viewport to the full page height so nothing is clipped
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
