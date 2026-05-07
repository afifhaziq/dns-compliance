package screenshot

import (
	"context"
	"math"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// AllocatorOptions are the chromedp ExecAllocatorOptions used by Capture and
// CaptureWithAllocator. Exported so callers can create a shared allocator with
// the same flags.
var AllocatorOptions = append(
	chromedp.DefaultExecAllocatorOptions[:],
	chromedp.Flag("headless", true),
	chromedp.Flag("no-sandbox", true),
	chromedp.Flag("disable-dev-shm-usage", true),
)

// Capture launches a headless Chrome instance, navigates to rawURL, and returns
// a full-page PNG screenshot. The context controls the total time budget;
// cancelling it will abort the browser process.
func Capture(ctx context.Context, rawURL string) ([]byte, error) {
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, AllocatorOptions...)
	defer allocCancel()
	return CaptureWithAllocator(ctx, allocCtx, rawURL, 5*time.Second)
}

// CaptureWithAllocator is like Capture but uses an existing allocator context
// instead of spawning a new Chrome process. Use this with a shared allocator to
// avoid per-URL process startup overhead. waitIdle is the maximum time to wait
// for network idle after navigation; the screenshot is taken immediately when
// idle is detected or after waitIdle elapses, whichever comes first.
func CaptureWithAllocator(ctx, allocCtx context.Context, rawURL string, waitIdle time.Duration) ([]byte, error) {
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	defer tabCancel()

	// Copy the per-URL deadline from ctx into the tab context, which is derived
	// from allocCtx and would otherwise ignore the caller's timeout.
	if deadline, ok := ctx.Deadline(); ok {
		var dl context.CancelFunc
		tabCtx, dl = context.WithDeadline(tabCtx, deadline)
		defer dl()
	}

	// Register the networkIdle listener before navigating so the event is never missed.
	idle := make(chan struct{}, 1)
	chromedp.ListenTarget(tabCtx, func(ev interface{}) {
		if e, ok := ev.(*page.EventLifecycleEvent); ok && e.Name == "networkIdle" {
			select {
			case idle <- struct{}{}:
			default:
			}
		}
	})

	var pageBuf []byte
	if err := chromedp.Run(tabCtx,
		chromedp.EmulateViewport(1920, 1080),
		page.SetLifecycleEventsEnabled(true),
		chromedp.Navigate(rawURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			select {
			case <-idle:
			case <-time.After(waitIdle):
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, contentSize, _, _, _, err := page.GetLayoutMetrics().Do(ctx)
			if err != nil {
				return err
			}
			fullH := int64(math.Ceil(contentSize.Height))
			if err := emulation.SetDeviceMetricsOverride(1920, fullH, 1, false).Do(ctx); err != nil {
				return err
			}
			pageBuf, err = page.CaptureScreenshot().
				WithFormat(page.CaptureScreenshotFormatPng).
				WithCaptureBeyondViewport(true).
				Do(ctx)
			return err
		}),
	); err != nil {
		return nil, err
	}

	// Wrap the page screenshot in a Chrome-like browser mockup.
	// Fall back to the raw screenshot if framing fails.
	framed, err := addBrowserFrame(tabCtx, pageBuf, rawURL)
	if err != nil {
		return pageBuf, nil
	}
	return framed, nil
}
