package screenshot

import (
	"context"
	"math"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// realisticUA mimics a regular desktop Chrome on Windows so sites don't serve
// blank pages to the "HeadlessChrome" user-agent.
const realisticUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"

// AllocatorOptions are the chromedp ExecAllocatorOptions used by Capture and
// CaptureWithAllocator. Exported so callers can create a shared allocator with
// the same flags.
var AllocatorOptions = append(
	chromedp.DefaultExecAllocatorOptions[:],
	chromedp.Flag("headless", true),
	chromedp.Flag("no-sandbox", true),
	chromedp.Flag("disable-dev-shm-usage", true),
	// Prevents Chrome from advertising automation via navigator flags and
	// DOM properties that many sites check to serve blank/restricted pages.
	chromedp.Flag("disable-blink-features", "AutomationControlled"),
)

// Capture launches a headless Chrome instance, navigates to rawURL, and returns
// a full-page PNG screenshot. The context controls the total time budget;
// cancelling it will abort the browser process.
func Capture(ctx context.Context, rawURL string) ([]byte, error) {
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, AllocatorOptions...)
	defer allocCancel()
	return CaptureWithAllocator(ctx, allocCtx, rawURL, 5*time.Second, 2*time.Second)
}

// CaptureWithAllocator is like Capture but uses an existing allocator context
// instead of spawning a new Chrome process. Use this with a shared allocator to
// avoid per-URL process startup overhead. waitIdle is the maximum time to wait
// for network idle after navigation; the screenshot is taken immediately when
// idle is detected or after waitIdle elapses, whichever comes first.
func CaptureWithAllocator(ctx, allocCtx context.Context, rawURL string, waitIdle, postIdleSleep time.Duration) ([]byte, error) {
	// WithErrorf suppresses chromedp's internal cleanup logs (e.g. "could not
	// retrieve document root: context deadline exceeded") that fire when a tab
	// is cancelled mid-operation. Errors are surfaced via return values instead.
	tabCtx, tabCancel := chromedp.NewContext(allocCtx,
		chromedp.WithErrorf(func(string, ...interface{}) {}),
	)
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
		// Set realistic UA, Accept-Language, and platform so sites like
		// Facebook see a normal Windows Chrome rather than a headless bot.
		chromedp.ActionFunc(func(ctx context.Context) error {
			return emulation.SetUserAgentOverride(realisticUA).
				WithAcceptLanguage("en-US,en;q=0.9").
				WithPlatform("Win32").
				Do(ctx)
		}),
		// Inject before any page script runs to hide all common automation
		// fingerprints (webdriver flag, missing plugins, missing chrome object,
		// permission query behaviour) that Facebook and similar sites check.
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`
				Object.defineProperty(navigator,'webdriver',{get:()=>undefined});
				Object.defineProperty(navigator,'plugins',{get:()=>Object.assign([],{length:5})});
				Object.defineProperty(navigator,'languages',{get:()=>['en-US','en']});
				window.chrome={runtime:{}};
				const origQuery=window.navigator.permissions.query;
				window.navigator.permissions.query=params=>
					params.name==='notifications'
						? Promise.resolve({state:Notification.permission})
						: origQuery(params);
			`).Do(ctx)
			return err
		}),
		page.SetLifecycleEventsEnabled(true),
		chromedp.Navigate(rawURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			select {
			case <-idle:
			case <-time.After(waitIdle):
			case <-ctx.Done():
				return ctx.Err()
			}
			if postIdleSleep > 0 {
				select {
				case <-time.After(postIdleSleep):
				case <-ctx.Done():
					return ctx.Err()
				}
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
