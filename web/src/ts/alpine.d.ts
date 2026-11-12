// Alpine 3 ships no type declarations; this is the small surface the app uses.
declare module "alpinejs" {
  interface AlpineStatic {
    data(name: string, factory: (...args: never[]) => object): void;
    store(name: string, value?: object): unknown;
    start(): void;
  }
  const Alpine: AlpineStatic;
  export default Alpine;
}
