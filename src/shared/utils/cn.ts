// Utility function to merge class names
// Handles conditional classes and removes duplicates

export type ClassValue = string | number | bigint | boolean | undefined | null;

export function cn(...classes: ClassValue[]): string {
  return classes.filter(Boolean).join(" ").replace(/\s+/g, " ").trim();
}
