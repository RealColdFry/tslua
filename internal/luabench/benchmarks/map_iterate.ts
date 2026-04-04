export default function (): number {
  const map = new Map<number, number>();
  for (let i = 0; i < 3000; i++) {
    map.set(i, i * 2);
  }
  let sum = 0;
  for (const [, value] of map) {
    sum += value;
  }
  return sum;
}
