import * as React from 'react';

import {
  Progress as ProgressPrimitive,
  ProgressTrack as ProgressTrackPrimitive,
  ProgressIndicator as ProgressIndicatorPrimitive,
  type ProgressProps as ProgressPrimitiveProps,
  type ProgressTrackProps as ProgressTrackPrimitiveProps,
} from '@/components/animate-ui/primitives/base/progress';
import { cn } from '@/lib/utils';

type ProgressProps = ProgressPrimitiveProps;

function Progress(props: ProgressProps) {
  return <ProgressPrimitive {...props} />;
}

type ProgressTrackProps = ProgressTrackPrimitiveProps;

function ProgressTrack({ className, ...props }: ProgressTrackProps) {
  return (
    <ProgressTrackPrimitive
      className={cn(
        'bg-stone-border relative h-1 w-full overflow-hidden rounded-full',
        className,
      )}
      {...props}
    >
      <ProgressIndicatorPrimitive className="bg-ink rounded-full h-full w-full flex-1" />
    </ProgressTrackPrimitive>
  );
}

export {
  Progress,
  ProgressTrack,
  type ProgressProps,
  type ProgressTrackProps,
};
