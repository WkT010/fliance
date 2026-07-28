import { Header } from './Header';

export function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-screen flex-col bg-nexa-950 text-nexa-100">
      <Header />
      <main className="flex-1 overflow-hidden">{children}</main>
    </div>
  );
}
