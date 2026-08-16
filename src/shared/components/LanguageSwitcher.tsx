"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { createPortal } from "react-dom";
import { LOCALES, LOCALE_COOKIE, normalizeLocale } from "@/i18n/config";
import { reloadTranslations } from "@/i18n/runtime";

const subscribeMounted = () => () => {};
const getMountedSnapshot = () => true;
const getMountedServerSnapshot = () => false;
const getPortalTarget = () =>
  typeof document === "undefined" ? null : document.body;
const getPortalServerSnapshot = () => null;
const localeListeners = new Set();

const subscribeLocale = (listener) => {
  localeListeners.add(listener);
  return () => localeListeners.delete(listener);
};

function getLocaleFromCookie() {
  if (typeof document === "undefined") return "en";
  const cookie = document.cookie
    .split(";")
    .find((c) => c.trim().startsWith(`${LOCALE_COOKIE}=`));
  const value = cookie ? decodeURIComponent(cookie.split("=")[1]) : "en";
  return normalizeLocale(value);
}

const getLocaleSnapshot = () => getLocaleFromCookie();
const getLocaleServerSnapshot = () => "en";

function notifyLocaleListeners() {
  for (const listener of localeListeners) listener();
}

const LOCALE_INFO = {
  en: { name: "English", flag: "🇺🇸" },
  vi: { name: "Tiếng Việt", flag: "🇻🇳" },
  "zh-CN": { name: "简体中文", flag: "🇨🇳" },
  "zh-TW": { name: "繁體中文", flag: "🇹🇼" },
  ja: { name: "日本語", flag: "🇯🇵" },
  "pt-BR": { name: "Português (Brasil)", flag: "🇧🇷" },
  "pt-PT": { name: "Português (Portugal)", flag: "🇵🇹" },
  ko: { name: "한국어", flag: "🇰🇷" },
  es: { name: "Español", flag: "🇪🇸" },
  de: { name: "Deutsch", flag: "🇩🇪" },
  fr: { name: "Français", flag: "🇫🇷" },
  he: { name: "עברית", flag: "🇮🇱" },
  ar: { name: "العربية", flag: "🇸🇦" },
  ru: { name: "Русский", flag: "🇷🇺" },
  pl: { name: "Polski", flag: "🇵🇱" },
  cs: { name: "Čeština", flag: "🇨🇿" },
  nl: { name: "Nederlands", flag: "🇳🇱" },
  tr: { name: "Türkçe", flag: "🇹🇷" },
  uk: { name: "Українська", flag: "🇺🇦" },
  tl: { name: "Tagalog", flag: "🇵🇭" },
  id: { name: "Indonesia", flag: "🇮🇩" },
  th: { name: "ไทย", flag: "🇹🇭" },
  km: { name: "ខ្មែរ", flag: "🇰🇭" },
  hi: { name: "हिन्दी", flag: "🇮🇳" },
  bn: { name: "বাংলা", flag: "🇧🇩" },
  ur: { name: "اردو", flag: "🇵🇰" },
  ro: { name: "Română", flag: "🇷🇴" },
  sv: { name: "Svenska", flag: "🇸🇪" },
  it: { name: "Italiano", flag: "🇮🇹" },
  el: { name: "Ελληνικά", flag: "🇬🇷" },
  hu: { name: "Magyar", flag: "🇭🇺" },
  fi: { name: "Suomi", flag: "🇫🇮" },
  da: { name: "Dansk", flag: "🇩🇰" },
  no: { name: "Norsk", flag: "🇳🇴" },
  fa: { name: "فارسی", flag: "🇮🇷" },
};

const getLocaleInfo = (locale) =>
  LOCALE_INFO[locale] || { name: locale, flag: "🌐" };

export default function LanguageSwitcher({
  className = "",
  isOpen: controlledOpen,
  onClose,
  hideTrigger = false,
}) {
  const locale = useSyncExternalStore(
    subscribeLocale,
    getLocaleSnapshot,
    getLocaleServerSnapshot,
  );
  const mounted = useSyncExternalStore(
    subscribeMounted,
    getMountedSnapshot,
    getMountedServerSnapshot,
  );
  const portalTarget = useSyncExternalStore(
    subscribeMounted,
    getPortalTarget,
    getPortalServerSnapshot,
  );
  const [isPending, setIsPending] = useState(false);
  const [internalOpen, setInternalOpen] = useState(false);
  const modalRef = useRef(null);

  const isControlled = typeof controlledOpen === "boolean";
  const isOpen = isControlled ? controlledOpen : internalOpen;
  const setIsOpen = useCallback(
    (value, nextLocale = locale) => {
      if (isControlled) {
        if (!value && onClose) onClose(nextLocale);
      } else {
        setInternalOpen(value);
      }
    },
    [isControlled, locale, onClose],
  );

  const setIsOpenRef = useRef(setIsOpen);
  useEffect(() => {
    setIsOpenRef.current = setIsOpen;
  }, [setIsOpen]);

  useEffect(() => {
    if (!isOpen) return undefined;
    const handleClickOutside = (event) => {
      if (modalRef.current && !modalRef.current.contains(event.target)) {
        setIsOpenRef.current(false);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, [isOpen]);

  const handleSetLocale = useCallback(
    async (nextLocale) => {
      if (nextLocale === locale || isPending) return;

      setIsPending(true);
      try {
        const response = await fetch("/api/locale", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ locale: nextLocale }),
        });
        if (!response.ok) throw new Error(`Locale update failed: ${response.status}`);
        await reloadTranslations();
        notifyLocaleListeners();
        setIsOpen(false, nextLocale);
      } catch (err) {
        console.error("Failed to set locale:", err);
      } finally {
        setIsPending(false);
      }
    },
    [isPending, locale, setIsOpen],
  );

  const portal = (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      data-i18n-skip="true"
    >
      <button
        type="button"
        aria-label="Close language menu"
        className="absolute inset-0 bg-black/30 backdrop-blur-sm"
        onClick={() => setIsOpen(false)}
      />
      <div
        ref={modalRef}
        className="relative w-full bg-surface border border-black/10 dark:border-white/10 rounded-xl shadow-2xl animate-in fade-in zoom-in-95 transition-opacity duration-200 max-w-2xl flex flex-col max-h-[80vh]"
      >
        <div className="flex items-center justify-between p-3 border-b border-black/5 dark:border-white/5">
          <h2 className="text-lg font-semibold text-text-main">Select Language</h2>
          <button
            type="button"
            onClick={() => setIsOpen(false)}
            className="p-1.5 rounded-lg text-text-muted hover:bg-black/5 dark:hover:bg-white/5 transition-colors"
            aria-label="Close"
          >
            <span className="material-symbols-outlined text-[20px]">close</span>
          </button>
        </div>
        <div className="p-6 overflow-y-auto flex-1">
          <div className="grid grid-cols-[repeat(auto-fill,minmax(100px,1fr))] gap-2">
            {LOCALES.map((item) => {
              const active = locale === item;
              const info = getLocaleInfo(item);
              return (
                <button
                  type="button"
                  key={item}
                  onClick={() => handleSetLocale(item)}
                  disabled={isPending}
                  className={`flex flex-col items-center justify-start gap-1 px-2 py-3 rounded-lg text-xs font-medium transition-colors w-full ${
                    active
                      ? "bg-primary/15 text-primary ring-2 ring-primary"
                      : "text-text-main hover:bg-black/5 dark:hover:bg-white/5"
                  } ${isPending ? "opacity-70 cursor-wait" : ""}`}
                  title={info.name}
                >
                  <span className="text-2xl">{info.flag}</span>
                  <span className="text-center leading-tight line-clamp-2 h-8 flex items-center">
                    {info.name}
                  </span>
                  {active && (
                    <span className="material-symbols-outlined text-sm">check</span>
                  )}
                </button>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );

  return (
    <div className={className}>
      {!hideTrigger && (
        <button
          type="button"
          onClick={() => setIsOpen(!isOpen)}
          disabled={isPending}
          className="flex items-center gap-2 px-3 py-2 rounded-lg text-text-muted hover:text-text-main hover:bg-surface/60 transition-colors"
          title="Language"
          data-i18n-skip="true"
        >
          <span className="material-symbols-outlined text-[20px]">language</span>
          <span className="text-sm font-medium">{getLocaleInfo(locale).name}</span>
          <span className="text-lg">{getLocaleInfo(locale).flag}</span>
        </button>
      )}
      {mounted && isOpen && portalTarget ? createPortal(portal, portalTarget) : null}
    </div>
  );
}
