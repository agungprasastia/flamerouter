/**
 * API utility functions for making HTTP requests
 */

const DEFAULT_HEADERS: Record<string, string> = {
  "Content-Type": "application/json",
};

export interface FetchOptions extends RequestInit {
  headers?: Record<string, string>;
}

export interface ApiError extends Error {
  status?: number;
  data?: any;
}

/**
 * Make a GET request
 */
export async function get(url: string, options: FetchOptions = {}) {
  const response = await fetch(url, {
    method: "GET",
    credentials: options.credentials || "include",
    headers: { ...DEFAULT_HEADERS, ...options.headers },
    ...options,
  });
  return handleResponse(response);
}

/**
 * Make a POST request
 */
export async function post(url: string, data?: any, options: FetchOptions = {}) {
  const response = await fetch(url, {
    method: "POST",
    credentials: options.credentials || "include",
    headers: { ...DEFAULT_HEADERS, ...options.headers },
    body: JSON.stringify(data),
    ...options,
  });
  return handleResponse(response);
}

/**
 * Make a PUT request
 */
export async function put(url: string, data?: any, options: FetchOptions = {}) {
  const response = await fetch(url, {
    method: "PUT",
    credentials: options.credentials || "include",
    headers: { ...DEFAULT_HEADERS, ...options.headers },
    body: JSON.stringify(data),
    ...options,
  });
  return handleResponse(response);
}

/**
 * Make a DELETE request
 */
export async function del(url: string, options: FetchOptions = {}) {
  const response = await fetch(url, {
    method: "DELETE",
    credentials: options.credentials || "include",
    headers: { ...DEFAULT_HEADERS, ...options.headers },
    ...options,
  });
  return handleResponse(response);
}

/**
 * Handle API response
 */
async function handleResponse(response: Response) {
  const data = await response.json();

  if (!response.ok) {
    const error = new Error(data.error || "An error occurred") as ApiError;
    error.status = response.status;
    error.data = data;
    throw error;
  }

  return data;
}

const api = { get, post, put, del };
export default api;
