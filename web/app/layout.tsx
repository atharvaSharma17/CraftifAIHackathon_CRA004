import type { Metadata } from 'next';
import './globals.css';
export const metadata: Metadata = { title: 'NavForge | Warehouse routing', description: 'Vision-aware indoor navigation decision support. Simulated vehicle, calibrated camera observations.' };
export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <html lang="en"><body>{children}</body></html>;
}
