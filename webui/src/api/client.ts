import type { GraphResponse, NodeResponse, HealthResponse } from '../types';

const API_BASE = '/api';

class ApiError extends Error {
  status: number;
  statusText: string;

  constructor(message: string, status: number, statusText: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.statusText = statusText;
  }
}

async function request<T>(endpoint: string, options?: RequestInit): Promise<T> {
  const url = `${API_BASE}${endpoint}`;

  const response = await fetch(url, {
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
    ...options,
  });

  if (!response.ok) {
    throw new ApiError(
      `Request failed: ${response.statusText}`,
      response.status,
      response.statusText
    );
  }

  return response.json();
}

export const api = {
  async getGraph(): Promise<GraphResponse> {
    return request<GraphResponse>('/graph');
  },

  async getNode(id: string): Promise<NodeResponse> {
    return request<NodeResponse>(`/node/${encodeURIComponent(id)}`);
  },

  async getHealth(): Promise<HealthResponse> {
    return request<HealthResponse>('/health');
  },
};

export { ApiError };
