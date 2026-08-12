import * as React from 'react';
import { Folder as FolderIconDefault, FileText as FileIconDefault } from 'lucide-react';

import {
  Files as FilesPrimitive,
  FilesHighlight as FilesHighlightPrimitive,
  FolderItem as FolderItemPrimitive,
  FolderHeader as FolderHeaderPrimitive,
  FolderTrigger as FolderTriggerPrimitive,
  FolderHighlight as FolderHighlightPrimitive,
  Folder as FolderPrimitive,
  FolderContent as FolderContentPrimitive,
  FileHighlight as FileHighlightPrimitive,
  File as FilePrimitive,
  FileIcon as FileIconPrimitive,
  useFolder,
  type FilesProps as FilesPrimitiveProps,
  type FolderItemProps as FolderItemPrimitiveProps,
  type FolderContentProps as FolderContentPrimitiveProps,
} from '@/components/animate-ui/primitives/radix/files';
import { cn } from '@/lib/utils';

type FilesProps = FilesPrimitiveProps;

function Files({ className, children, ...props }: FilesProps) {
  return (
    <FilesPrimitive className={cn('w-full', className)} {...props}>
      <FilesHighlightPrimitive className="bg-stone-panel rounded-lg pointer-events-none">
        {children}
      </FilesHighlightPrimitive>
    </FilesPrimitive>
  );
}

type SubFilesProps = FilesProps;

function SubFiles(props: SubFilesProps) {
  return <FilesPrimitive {...props} />;
}

type FolderItemProps = FolderItemPrimitiveProps;

function FolderItem(props: FolderItemProps) {
  return <FolderItemPrimitive {...props} />;
}

// The leading icon is semantic per-row (law/citation/category/...), not a
// generic folder — so unlike a real file tree there's no "open" variant of
// it to swap to. A chevron carries the expand/collapse affordance instead.
function FolderChevron() {
  const { isOpen } = useFolder();
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 12 12"
      fill="none"
      aria-hidden="true"
      className={cn(
        'shrink-0 text-stone-muted transition-transform duration-150 ease-snappy',
        isOpen && 'rotate-90',
      )}
    >
      <path d="M4.5 2.5L7.5 6L4.5 9.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

type FolderTriggerProps = React.ComponentProps<'div'> & {
  icon?: React.ElementType;
  /** Row actions (add/edit/delete) — a sibling of the toggle button, not
      inside it, so clicking them doesn't also expand/collapse. Both still
      sit inside the same FolderHighlightPrimitive wrapper (a single div)
      so the hover-highlight bounds cover the whole row, not just the
      label — the highlight box is measured off that wrapper's own
      getBoundingClientRect(), so anything outside it gets left out. */
  actions?: React.ReactNode;
};

function FolderTrigger({
  children,
  className,
  icon: Icon = FolderIconDefault,
  actions,
  ...props
}: FolderTriggerProps) {
  return (
    <FolderHeaderPrimitive>
      <FolderHighlightPrimitive>
        <div className="flex items-center gap-1">
          <FolderTriggerPrimitive className="flex-1 min-w-0 text-start">
            <FolderPrimitive className="flex items-center gap-2 p-2 pointer-events-none min-w-0">
              <FolderChevron />
              <Icon className="size-4.5 shrink-0 text-stone-muted" />
              <div className={cn('text-sm min-w-0', className)} {...props}>
                {children}
              </div>
            </FolderPrimitive>
          </FolderTriggerPrimitive>
          {actions && <div className="flex items-center gap-1 pr-1 shrink-0">{actions}</div>}
        </div>
      </FolderHighlightPrimitive>
    </FolderHeaderPrimitive>
  );
}

type FolderContentProps = FolderContentPrimitiveProps;

function FolderContent(props: FolderContentProps) {
  return (
    <div className="relative ml-5 before:absolute before:-left-2.5 before:inset-y-0 before:w-px before:h-full before:bg-stone-border">
      <FolderContentPrimitive {...props} />
    </div>
  );
}

type FileItemProps = React.ComponentProps<'div'> & {
  icon?: React.ElementType;
  actions?: React.ReactNode;
};

function FileItem({
  icon: Icon = FileIconDefault,
  className,
  children,
  actions,
  ...props
}: FileItemProps) {
  return (
    <FileHighlightPrimitive>
      <FilePrimitive className="flex items-center justify-between gap-2 p-2 pointer-events-none">
        <div className="flex items-center gap-2 min-w-0">
          {/* w-3 spacer stands in for the folder row's chevron, so leaf
              icons line up under folder icons instead of sitting flush left. */}
          <span className="w-3 shrink-0" />
          <FileIconPrimitive>
            <Icon className="size-4.5 shrink-0 text-stone-muted" />
          </FileIconPrimitive>
          <div className={cn('text-sm min-w-0', className)} {...props}>
            {children}
          </div>
        </div>
        {actions && <div className="flex items-center gap-1 shrink-0 pointer-events-auto">{actions}</div>}
      </FilePrimitive>
    </FileHighlightPrimitive>
  );
}

export {
  Files,
  FolderItem,
  FolderTrigger,
  FolderContent,
  FileItem,
  SubFiles,
  type FilesProps,
  type FolderItemProps,
  type FolderTriggerProps,
  type FolderContentProps,
  type FileItemProps,
  type SubFilesProps,
};
