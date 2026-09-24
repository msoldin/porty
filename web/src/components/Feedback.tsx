import type { ComponentChildren } from "preact";

export function Badge({
  children,
  tone = "neutral",
  dot = false,
}: {
  children: ComponentChildren;
  tone?: string;
  dot?: boolean;
}) {
  return (
    <span class={`badge ${tone}`}>
      {dot && <i class="badge-dot" aria-hidden="true" />}
      {children}
    </span>
  );
}
export function Notice({ children }: { children: ComponentChildren }) {
  return (
    <div class="notice" role="alert">
      {children}
    </div>
  );
}
export function Empty({ children }: { children: ComponentChildren }) {
  return <p class="empty">{children}</p>;
}
