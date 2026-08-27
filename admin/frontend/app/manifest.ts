import type { MetadataRoute } from 'next';

export const dynamic = 'force-static';

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: 'OnePlus Router OS',
    short_name: 'Router OS',
    description: '一加 7 Pro All-in-One 软路由管理后台',
    start_url: '/',
    display: 'standalone',
    background_color: '#07151e',
    theme_color: '#07151e',
    lang: 'zh-CN',
  };
}
