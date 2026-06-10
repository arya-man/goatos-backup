import { createAppApiClient } from "@goatos/api-client";

export function getApiClientSmokeConfig() {
  const baseUrl = process.env.GOATOS_API_BASE_URL ?? process.env.NEXT_PUBLIC_GOATOS_API_BASE_URL ?? "http://localhost:8080";
  const bearerToken = process.env.GOATOS_BEARER_TOKEN;

  createAppApiClient({
    baseUrl,
    bearerToken,
  });

  return {
    baseUrl,
    hasBearerToken: Boolean(bearerToken),
  };
}
