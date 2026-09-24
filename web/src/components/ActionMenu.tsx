import { useEffect, useRef, useState } from "preact/hooks";
import { Icon } from "./Icon";

export type ActionMenuItem = {
  id: string;
  label: string;
  disabled?: boolean;
  reason?: string;
};

export function ActionMenu({
  label,
  items,
  onSelect,
  disabled = false,
}: {
  label: string;
  items: ActionMenuItem[];
  onSelect: (id: string) => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    menuRef.current
      ?.querySelector<HTMLButtonElement>(
        '[role="menuitem"][aria-disabled="false"]',
      )
      ?.focus();
    function closeOutside(event: PointerEvent) {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    }
    document.addEventListener("pointerdown", closeOutside);
    return () => document.removeEventListener("pointerdown", closeOutside);
  }, [open]);

  function moveFocus(key: string) {
    const enabled = Array.from(
      menuRef.current?.querySelectorAll<HTMLButtonElement>(
        '[role="menuitem"][aria-disabled="false"]',
      ) || [],
    );
    if (!enabled.length) return;
    const index = enabled.indexOf(document.activeElement as HTMLButtonElement);
    const next =
      key === "Home"
        ? 0
        : key === "End"
          ? enabled.length - 1
          : key === "ArrowUp"
            ? (index + enabled.length - 1) % enabled.length
            : (index + 1) % enabled.length;
    enabled[next].focus();
  }

  return (
    <div class="action-menu" ref={rootRef}>
      <button
        type="button"
        class="action-menu-trigger"
        ref={triggerRef}
        disabled={disabled}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        onKeyDown={(event) => {
          if (
            event.key === "ArrowDown" ||
            event.key === "Enter" ||
            event.key === " "
          ) {
            event.preventDefault();
            setOpen(true);
          }
          if (event.key === "Escape") setOpen(false);
        }}
      >
        {label}
        <Icon name="Chevron" />
      </button>
      {open && (
        <div
          class="action-menu-popover"
          role="menu"
          ref={menuRef}
          onKeyDown={(event) => {
            if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
              event.preventDefault();
              moveFocus(event.key);
            } else if (event.key === "Escape") {
              event.preventDefault();
              setOpen(false);
              triggerRef.current?.focus();
            }
          }}
        >
          {items.map((item) => (
            <button
              key={item.id}
              type="button"
              role="menuitem"
              aria-disabled={item.disabled ? "true" : "false"}
              title={item.disabled ? item.reason : undefined}
              tabIndex={-1}
              onClick={() => {
                if (item.disabled) return;
                setOpen(false);
                triggerRef.current?.focus();
                onSelect(item.id);
              }}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
