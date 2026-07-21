export function Card({ className = "", as: Component = "div", ...props }) {
  return (
    <Component
      className={`rounded-[14px] border border-[var(--border)] bg-[var(--surface)] shadow-[var(--shadow-soft)] ${className}`}
      {...props}
    />
  );
}
