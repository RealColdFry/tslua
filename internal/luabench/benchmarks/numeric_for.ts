export default function (): number {
  let sum = 0;
  for (let i = 0; i < 10_000; i++) {
    sum += i;
  }
  return sum;
}
