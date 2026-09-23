import type { ComponentChildren } from "preact";

export function Badge({
  children,
  tone = "neutral",
}: {
  children: ComponentChildren;
  tone?: string;
}) {
  return <span class={`badge ${tone}`}>{children}</span>;
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
