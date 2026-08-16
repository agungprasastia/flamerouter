export type StatusVariant = "default" | "success" | "error";

export function getStatusVariant(
  isActive?: boolean | null,
  effectiveStatus?: string | null
): StatusVariant {
  if (isActive === false) return "default";
  if (effectiveStatus === "active" || effectiveStatus === "success")
    return "success";
  if (
    effectiveStatus === "error" ||
    effectiveStatus === "expired" ||
    effectiveStatus === "unavailable"
  )
    return "error";
  return "default";
}
