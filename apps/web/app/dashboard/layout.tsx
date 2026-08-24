import { notFound } from 'next/navigation';

// Engine debug console. It only talks to a local engine over
// ws://localhost:8765 -- which the production CSP blocks anyway -- so shipping
// it on the public site exposed internal tooling and nothing else. Keep it
// available for local engine work, 404 it in production.
export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  if (process.env.NODE_ENV === 'production') {
    notFound();
  }
  return children;
}
