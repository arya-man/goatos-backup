/** @type {import('next').NextConfig} */
const nextConfig = {
  basePath: "/dashboard",
  devIndicators: false,
  output: "standalone",
  transpilePackages: ["@goatos/api-client"],
};

export default nextConfig;
