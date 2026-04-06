export default function (): number[] {
  const arr: number[] = [];
  for (let i = 0; i < 10000; i++) {
    arr.push(i * i);
  }
  return arr;
}
