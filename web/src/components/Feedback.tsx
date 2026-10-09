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
export function Notice({
  children,
  tone = "warning",
  role = "alert",
}: {
  children: ComponentChildren;
  tone?: string;
  role?: "alert" | "status";
}) {
  return (
    <div class={`notice ${tone}`} role={role}>
      {children}
    </div>
  );
}
export function Empty({ children }: { children: ComponentChildren }) {
  return <p class="empty">{children}</p>;
}
