export interface SunburstNode {
  name: string;
  value?: number;
  /** Optional layout size (e.g. log1p(value)) — arc angle only; value still drives labels/tooltips. */
  weight?: number;
  color?: string;
  /** Optional fill override for patterns/gradients (e.g., "url(#patternId)") */
  fill?: string;
  children?: SunburstNode[];
}
