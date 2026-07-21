import { Desktop, Moon, Sun } from "@phosphor-icons/react";
import { useTheme } from "../store/theme";
import { Button } from "./Button";

const labels = {
  light: { text: "Light", Icon: Sun },
  dark: { text: "Dark", Icon: Moon },
  system: { text: "System", Icon: Desktop },
};

export function ThemeToggle() {
  const theme = useTheme((s) => s.theme);
  const cycleTheme = useTheme((s) => s.cycleTheme);
  const { text, Icon } = labels[theme] || labels.system;

  return (
    <Button type="button" variant="secondary" size="sm" onClick={cycleTheme} aria-label={`Theme: ${text}`}>
      <Icon size={16} weight="duotone" />
      {text}
    </Button>
  );
}
