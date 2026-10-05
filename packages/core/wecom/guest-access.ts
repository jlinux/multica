/** Limits mirror the server policy validation; no wildcard grants are accepted. */
export function validateWecomGuestGroup(
  groupId: string,
  existing: readonly string[],
): "empty" | "duplicate" | "wildcard" | "too_long" | "too_many" | null {
  if (!groupId.trim()) return "empty";
  if (/[*?]/.test(groupId)) return "wildcard";
  if (new TextEncoder().encode(groupId).length > 256) return "too_long";
  if (existing.includes(groupId)) return "duplicate";
  if (existing.length >= 100) return "too_many";
  return null;
}
