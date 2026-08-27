import type { Metadata, Viewport } from 'next';
import '@xterm/xterm/css/xterm.css';
import './globals.css';
import './mihomo.css';

export const metadata: Metadata = {
  title: 'OnePlus Router OS',
  description: '一加 7 Pro All-in-One 软路由管理后台',
  manifest: '/manifest.webmanifest',
  appleWebApp: { capable: true, title: 'OnePlus Router' },
};

export const viewport: Viewport = {
  themeColor: '#07151e',
  width: 'device-width',
  initialScale: 1,
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
