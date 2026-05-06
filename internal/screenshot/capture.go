package screenshot

import (
	"context"

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
		chromedp.Navigate(rawURL),
		chromedp.FullScreenshot(&buf, 90),
	); err != nil {
		return nil, err
	}
	return buf, nil
}
