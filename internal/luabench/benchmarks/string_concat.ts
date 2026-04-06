export default function (): string {
  let s = "";
  for (let i = 0; i < 1000; i++) {
    s += String(i);
  }
  return s;
}
