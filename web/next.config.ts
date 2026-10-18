import type { NextConfig } from 'next';
const config: NextConfig = {
	// Compression buffers the proxied SSE response and delays state delivery.
	compress: false,
  async rewrites() {
    return [{ source: '/api/:path*', destination: 'http://127.0.0.1:8080/api/:path*' }];
  },
};
export default config;
