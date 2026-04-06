export default function (): number {
  const set = new Set<number>();
  for (let i = 0; i < 3000; i++) {
    set.add(i);
  }
  let sum = 0;
  for (const v of set) {
    sum += v;
  }
  return sum;
}
