export default function (): number {
  const arr: number[] = [];
  for (let i = 0; i < 10000; i++) arr.push(i);
  let sum = 0;
  for (const v of arr) {
    sum += v;
  }
  return sum;
}
