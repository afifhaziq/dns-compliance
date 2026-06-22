import * as React from 'react';

import {
  PreviewCard,
  PreviewCardTrigger,
  PreviewCardPortal,
  PreviewCardPositioner,
  PreviewCardPopup,
  PreviewCardBackdrop,
  type PreviewCardProps,
  type PreviewCardTriggerProps,
  type PreviewCardPositionerProps,
  type PreviewCardPopupProps,
  type PreviewCardBackdropProps,
} from '@/components/animate-ui/primitives/base/preview-card';
import { cn } from '@/lib/utils';
import { getStrictContext } from '@/lib/get-strict-context';

const [PreviewLinkCardProvider, usePreviewLinkCard] =
  getStrictContext<{ href: string }>('PreviewLinkCardContext');

type PreviewLinkCardProps = PreviewCardProps & { href: string };

function PreviewLinkCard({ href, ...props }: PreviewLinkCardProps) {
  return (
    <PreviewLinkCardProvider value={{ href }}>
      <PreviewCard {...props} />
    </PreviewLinkCardProvider>
  );
}

type PreviewLinkCardTriggerProps = Omit<PreviewCardTriggerProps, 'render'> & {
  className?: string;
};

function PreviewLinkCardTrigger({ children, className, ...props }: PreviewLinkCardTriggerProps) {
  const { href } = usePreviewLinkCard();
  return (
    <PreviewCardTrigger
      render={
        <a
          href={href}
          target="_blank"
          rel="noopener noreferrer"
          className={cn('cursor-pointer', className)}
          onClick={e => e.stopPropagation()}
        />
      }
      {...props}
    >
      {children}
    </PreviewCardTrigger>
  );
}

type PreviewLinkCardPanelProps = PreviewCardPositionerProps &
  Pick<PreviewCardPopupProps, 'className' | 'style' | 'children'>;

function PreviewLinkCardPanel({
  className,
  align = 'center',
  sideOffset = 8,
  style,
  children,
  ...props
}: PreviewLinkCardPanelProps) {
  return (
    <PreviewCardPortal>
      <PreviewCardPositioner align={align} sideOffset={sideOffset} className="z-50" {...props}>
        <PreviewCardPopup
          className={cn(
            'origin-(--transform-origin) rounded-lg border shadow-xl outline-hidden overflow-hidden bg-white dark:bg-zinc-900',
            className,
          )}
          style={style}
        >
          {children}
        </PreviewCardPopup>
      </PreviewCardPositioner>
    </PreviewCardPortal>
  );
}

type PreviewLinkCardBackdropProps = PreviewCardBackdropProps;

function PreviewLinkCardBackdrop(props: PreviewLinkCardBackdropProps) {
  return <PreviewCardBackdrop {...props} />;
}

type PreviewLinkCardImageProps = Omit<React.ImgHTMLAttributes<HTMLImageElement>, 'src'>;

function PreviewLinkCardImage({ alt, className, ...props }: PreviewLinkCardImageProps) {
  const { href } = usePreviewLinkCard();
  const src = `https://api.microlink.io/?url=${encodeURIComponent(href)}&screenshot=true&meta=false&embed=screenshot.url`;
  return (
    <img
      src={src}
      alt={alt ?? `Preview of ${href}`}
      className={cn('w-72 h-44 object-cover object-top', className)}
      loading="lazy"
      {...props}
    />
  );
}

export {
  PreviewLinkCard,
  PreviewLinkCardTrigger,
  PreviewLinkCardPanel,
  PreviewLinkCardBackdrop,
  PreviewLinkCardImage,
  type PreviewLinkCardProps,
  type PreviewLinkCardTriggerProps,
  type PreviewLinkCardPanelProps,
  type PreviewLinkCardBackdropProps,
  type PreviewLinkCardImageProps,
};
