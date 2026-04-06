export default function (): number {
  const s = "abcdefghijklmnopqrstuvwxyz".repeat(100);
  let count = 0;
  for (const c of s) {
    count++;
  }
  return count;
}
