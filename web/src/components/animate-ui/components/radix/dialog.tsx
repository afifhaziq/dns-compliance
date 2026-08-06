import { XIcon } from 'lucide-react';

import {
  Dialog as DialogPrimitive,
  DialogContent as DialogContentPrimitive,
  DialogDescription as DialogDescriptionPrimitive,
  DialogFooter as DialogFooterPrimitive,
  DialogHeader as DialogHeaderPrimitive,
  DialogTitle as DialogTitlePrimitive,
  DialogTrigger as DialogTriggerPrimitive,
  DialogPortal as DialogPortalPrimitive,
  DialogOverlay as DialogOverlayPrimitive,
  DialogClose as DialogClosePrimitive,
  type DialogProps as DialogPrimitiveProps,
  type DialogContentProps as DialogContentPrimitiveProps,
  type DialogDescriptionProps as DialogDescriptionPrimitiveProps,
  type DialogFooterProps as DialogFooterPrimitiveProps,
  type DialogHeaderProps as DialogHeaderPrimitiveProps,
  type DialogTitleProps as DialogTitlePrimitiveProps,
  type DialogTriggerProps as DialogTriggerPrimitiveProps,
  type DialogOverlayProps as DialogOverlayPrimitiveProps,
  type DialogCloseProps as DialogClosePrimitiveProps,
} from '@/components/animate-ui/primitives/radix/dialog';
import { cn } from '@/lib/utils';

type DialogProps = DialogPrimitiveProps;

function Dialog(props: DialogProps) {
  return <DialogPrimitive {...props} />;
}

type DialogTriggerProps = DialogTriggerPrimitiveProps;

function DialogTrigger(props: DialogTriggerProps) {
  return <DialogTriggerPrimitive {...props} />;
}

type DialogCloseProps = DialogClosePrimitiveProps;

function DialogClose(props: DialogCloseProps) {
  return <DialogClosePrimitive {...props} />;
}

type DialogOverlayProps = DialogOverlayPrimitiveProps;

function DialogOverlay({ className, ...props }: DialogOverlayProps) {
  return (
    <DialogOverlayPrimitive
      className={cn('fixed inset-0 z-50 bg-black/50', className)}
      {...props}
    />
  );
}

type DialogContentProps = DialogContentPrimitiveProps & {
  showCloseButton?: boolean;
};

const IGNORED_OUTSIDE_SELECTOR =
  '[data-slot="select-content"], [data-slot="date-picker-panel"]';

// Elements portalled to document.body by components other than this Dialog
// (e.g. our custom Select's popover, or the date-picker's calendar panel)
// live outside the DialogContent DOM subtree even though they render
// visually on top of it. Radix's dismissable layer only checks DOM
// containment, so without this it treats any click inside one of those
// portals as an "outside" interaction and closes the dialog before the
// click (a date/option selection) can register.
//
// event.target alone isn't reliable here: by the time this fires, other
// document-level pointerdown-outside listeners (the date-picker's own) or
// an in-flight re-render can leave it pointing at a node that doesn't match
// what's actually on screen for this gesture. Prefer geometry - is the
// pointer physically over one of those portalled panels? - when the event
// carries coordinates (pointerdown does; focus events don't), since that
// can't be fooled by target-resolution quirks the way DOM containment can.
function isIgnoredOutsideTarget(event: {
  target: EventTarget | null;
  clientX?: number;
  clientY?: number;
}) {
  if (typeof event.clientX === "number" && typeof event.clientY === "number") {
    const panels = document.querySelectorAll<HTMLElement>(IGNORED_OUTSIDE_SELECTOR);
    for (const panel of panels) {
      const rect = panel.getBoundingClientRect();
      if (
        event.clientX >= rect.left &&
        event.clientX <= rect.right &&
        event.clientY >= rect.top &&
        event.clientY <= rect.bottom
      ) {
        return true;
      }
    }
  }
  return !!(event.target as HTMLElement | null)?.closest(IGNORED_OUTSIDE_SELECTOR);
}

function DialogContent({
  className,
  children,
  showCloseButton = true,
  onPointerDownOutside,
  onFocusOutside,
  onInteractOutside,
  ...props
}: DialogContentProps) {
  return (
    <DialogPortalPrimitive>
      <DialogOverlay />
      <DialogContentPrimitive
        className={cn(
          'bg-background fixed top-[50%] left-[50%] z-50 grid w-full max-w-[calc(100%-2rem)] max-h-[85vh] overflow-y-auto translate-x-[-50%] translate-y-[-50%] gap-4 rounded-lg border p-6 shadow-lg sm:max-w-lg',
          className,
        )}
        onPointerDownOutside={(e) => {
          const original = e.detail.originalEvent;
          if (isIgnoredOutsideTarget({ target: e.target, clientX: original.clientX, clientY: original.clientY })) {
            e.preventDefault();
            return;
          }
          onPointerDownOutside?.(e);
        }}
        onFocusOutside={(e) => {
          if (isIgnoredOutsideTarget({ target: e.target })) { e.preventDefault(); return; }
          onFocusOutside?.(e);
        }}
        onInteractOutside={(e) => {
          const original = e.detail.originalEvent;
          const coords =
            'clientX' in original
              ? { clientX: original.clientX, clientY: original.clientY }
              : {};
          if (isIgnoredOutsideTarget({ target: e.target, ...coords })) {
            e.preventDefault();
            return;
          }
          onInteractOutside?.(e);
        }}
        {...props}
      >
        {children}
        {showCloseButton && (
          <DialogClosePrimitive className="ring-offset-background focus:ring-ring data-[state=open]:bg-accent data-[state=open]:text-muted-foreground absolute top-4 right-4 rounded-xs opacity-70 transition-opacity hover:opacity-100 focus:ring-2 focus:ring-offset-2 focus:outline-hidden disabled:pointer-events-none [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4">
            <XIcon />
            <span className="sr-only">Close</span>
          </DialogClosePrimitive>
        )}
      </DialogContentPrimitive>
    </DialogPortalPrimitive>
  );
}

type DialogHeaderProps = DialogHeaderPrimitiveProps;

function DialogHeader({ className, ...props }: DialogHeaderProps) {
  return (
    <DialogHeaderPrimitive
      className={cn('flex flex-col gap-2 text-center sm:text-left', className)}
      {...props}
    />
  );
}

type DialogFooterProps = DialogFooterPrimitiveProps;

function DialogFooter({ className, ...props }: DialogFooterProps) {
  return (
    <DialogFooterPrimitive
      className={cn(
        'flex flex-col-reverse gap-2 sm:flex-row sm:justify-end',
        className,
      )}
      {...props}
    />
  );
}

type DialogTitleProps = DialogTitlePrimitiveProps;

function DialogTitle({ className, ...props }: DialogTitleProps) {
  return (
    <DialogTitlePrimitive
      className={cn('text-lg leading-none font-semibold', className)}
      {...props}
    />
  );
}

type DialogDescriptionProps = DialogDescriptionPrimitiveProps;

function DialogDescription({ className, ...props }: DialogDescriptionProps) {
  return (
    <DialogDescriptionPrimitive
      className={cn('text-muted-foreground text-sm', className)}
      {...props}
    />
  );
}

export {
  Dialog,
  DialogTrigger,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogFooter,
  DialogTitle,
  DialogDescription,
  type DialogProps,
  type DialogTriggerProps,
  type DialogCloseProps,
  type DialogContentProps,
  type DialogHeaderProps,
  type DialogFooterProps,
  type DialogTitleProps,
  type DialogDescriptionProps,
};
