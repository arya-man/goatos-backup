/** @type {import('next').NextConfig} */
const nextConfig = {
  devIndicators: false,
  output: "standalone",
  transpilePackages: ["@goatos/api-client"],
};

export default nextConfig;
