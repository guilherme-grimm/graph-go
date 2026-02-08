import { useEffect, useCallback } from 'react';

type KeyboardShortcut = {
  key: string;
  ctrl?: boolean;
  meta?: boolean;
  shift?: boolean;
  alt?: boolean;
  handler: (event: KeyboardEvent) => void;
};

export function useKeyboard(shortcuts: KeyboardShortcut[]) {
  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      // Don't trigger shortcuts when typing in inputs
      const target = event.target as HTMLElement;
      if (
        target.tagName === 'INPUT' ||
        target.tagName === 'TEXTAREA' ||
        target.isContentEditable
      ) {
        // Allow Escape to work even in inputs
        if (event.key !== 'Escape') return;
      }

      for (const shortcut of shortcuts) {
        const keyMatches = event.key.toLowerCase() === shortcut.key.toLowerCase();
        const shiftMatches = shortcut.shift ? event.shiftKey : !event.shiftKey;
        const altMatches = shortcut.alt ? event.altKey : !event.altKey;

        // For Cmd/Ctrl+K style shortcuts, check if the modifier matches
        const modifierRequired = shortcut.ctrl || shortcut.meta;
        const hasModifier = event.ctrlKey || event.metaKey;

        if (keyMatches && shiftMatches && altMatches) {
          if (modifierRequired && hasModifier) {
            event.preventDefault();
            shortcut.handler(event);
            return;
          } else if (!modifierRequired && !hasModifier) {
            shortcut.handler(event);
            return;
          }
        }
      }
    },
    [shortcuts]
  );

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);
}

// Convenience hook for common app shortcuts
export function useAppShortcuts({
  onSearch,
  onEscape,
}: {
  onSearch?: () => void;
  onEscape?: () => void;
}) {
  const shortcuts: KeyboardShortcut[] = [];

  if (onSearch) {
    shortcuts.push({
      key: 'k',
      ctrl: true,
      handler: onSearch,
    });
  }

  if (onEscape) {
    shortcuts.push({
      key: 'Escape',
      handler: onEscape,
    });
  }

  useKeyboard(shortcuts);
}
