'use client';

import * as React from 'react';
import { Progress as ProgressPrimitives } from '@base-ui-components/react/progress';
import { motion } from 'motion/react';

import { getStrictContext } from '@/lib/get-strict-context';

type ProgressContextType = {
  value: number;
};

const [ProgressProvider, useProgress] =
  getStrictContext<ProgressContextType>('ProgressContext');

type ProgressProps = React.ComponentProps<typeof ProgressPrimitives.Root>;

const Progress = (props: ProgressProps) => {
  return (
    <ProgressProvider value={{ value: props.value ?? 0 }}>
      <ProgressPrimitives.Root data-slot="progress" {...props} />
    </ProgressProvider>
  );
};

type ProgressIndicatorProps = React.ComponentProps<
  typeof MotionProgressIndicator
>;

const MotionProgressIndicator = motion.create(ProgressPrimitives.Indicator);

function ProgressIndicator({
  transition = { type: 'spring', stiffness: 100, damping: 30 },
  ...props
}: ProgressIndicatorProps) {
  const { value } = useProgress();

  return (
    <MotionProgressIndicator
      data-slot="progress-indicator"
      animate={{ width: `${value}%` }}
      transition={transition}
      {...props}
    />
  );
}

type ProgressTrackProps = React.ComponentProps<typeof ProgressPrimitives.Track>;

function ProgressTrack(props: ProgressTrackProps) {
  return <ProgressPrimitives.Track data-slot="progress-track" {...props} />;
}

export {
  Progress,
  ProgressIndicator,
  ProgressTrack,
  useProgress,
  type ProgressProps,
  type ProgressIndicatorProps,
  type ProgressTrackProps,
  type ProgressContextType,
};
