"use client";

import { DEFAULT_LOCALE, LOCALE_COOKIE, normalizeLocale, type SupportedLocale } from "./config";

type CustomTextNode = Text & {
  _originalText?: string;
};

let translationMap: Record<string, string> = {};
let currentLocale: SupportedLocale = DEFAULT_LOCALE;
let reloadCallbacks: Array<() => void> = [];

// Read locale from cookie
function getLocaleFromCookie(): SupportedLocale {
  if (typeof document === "undefined") return DEFAULT_LOCALE;
  const cookie = document.cookie
    .split(";")
    .find((c) => c.trim().startsWith(`${LOCALE_COOKIE}=`));
  const value = cookie
    ? decodeURIComponent(cookie.split("=")[1] ?? "")
    : DEFAULT_LOCALE;
  return normalizeLocale(value);
}

// Load translation map
async function loadTranslations(locale: SupportedLocale): Promise<void> {
  if (locale === "en") {
    translationMap = {};
    return;
  }

  try {
    const response = await fetch(`/i18n/literals/${locale}.json`);
    translationMap = (await response.json()) as Record<string, string>;
  } catch (err: unknown) {
    console.error("Failed to load translations:", err);
    translationMap = {};
  }
}

// Translate text - exported for use in components
export function translate(text: unknown): string {
  if (!text || typeof text !== "string") return typeof text === "string" ? text : "";
  const trimmed = text.trim();
  if (!trimmed) return text;
  if (currentLocale === "en") return text;
  return translationMap[trimmed] || text;
}

// Get current locale - exported for use in components
export function getCurrentLocale(): SupportedLocale {
  return currentLocale;
}

// Register callback for locale changes
export function onLocaleChange(callback: () => void): () => void {
  reloadCallbacks.push(callback);
  return () => {
    reloadCallbacks = reloadCallbacks.filter((cb) => cb !== callback);
  };
}

// Process text node
function processTextNode(node: Node): void {
  const textNode = node as CustomTextNode;
  if (!textNode.nodeValue || !textNode.nodeValue.trim()) return;

  // Skip if parent is script, style, code, or structural elements
  const parent = textNode.parentElement;
  if (!parent) return;

  // Skip if parent or any ancestor has data-i18n-skip attribute
  let element: HTMLElement | null = parent;
  while (element) {
    if (element.hasAttribute && element.hasAttribute("data-i18n-skip")) {
      return;
    }
    element = element.parentElement;
  }

  const tagName = parent.tagName?.toLowerCase();

  // Skip elements that don't allow text nodes
  const skipTags = [
    "script",
    "style",
    "code",
    "pre",
    "colgroup",
    "table",
    "thead",
    "tbody",
    "tfoot",
    "tr",
    "select",
    "datalist",
    "optgroup",
  ];

  if (skipTags.includes(tagName)) return;

  // Store original text if not already stored
  if (!textNode._originalText) {
    textNode._originalText = textNode.nodeValue;
  }

  // Use original text for translation
  const original = textNode._originalText;
  const translated = translate(original);

  // Only update if different to avoid unnecessary DOM mutations
  if (translated !== textNode.nodeValue) {
    textNode.nodeValue = translated;
  }
}

// Process all text nodes in element
function processElement(element: Element | null): void {
  if (!element) return;

  const walker = document.createTreeWalker(
    element,
    NodeFilter.SHOW_TEXT,
    null,
  );

  let node: Node | null;
  const nodesToProcess: Node[] = [];

  // Collect all nodes first to avoid live collection issues
  while ((node = walker.nextNode())) {
    nodesToProcess.push(node);
  }

  // Process collected nodes
  nodesToProcess.forEach(processTextNode);
}

// Initialize runtime i18n
export async function initRuntimeI18n(): Promise<void> {
  if (typeof window === "undefined") return;

  currentLocale = getLocaleFromCookie();
  await loadTranslations(currentLocale);

  // Process existing DOM
  processElement(document.body);

  // Watch for new nodes
  const observer = new MutationObserver((mutations) => {
    mutations.forEach((mutation) => {
      mutation.addedNodes.forEach((node) => {
        if (node.nodeType === Node.ELEMENT_NODE) {
          processElement(node as Element);
        } else if (node.nodeType === Node.TEXT_NODE) {
          processTextNode(node);
        }
      });
    });
  });

  observer.observe(document.body, {
    childList: true,
    subtree: true,
  });
}

// Reload translations when locale changes
export async function reloadTranslations(): Promise<void> {
  currentLocale = getLocaleFromCookie();
  await loadTranslations(currentLocale);

  // Notify all registered callbacks
  reloadCallbacks.forEach((callback) => callback());

  // Re-process entire DOM (will use stored original text)
  processElement(document.body);
}
