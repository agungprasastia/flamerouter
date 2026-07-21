import { useEffect, useState } from "react";
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts";
import { stats as fetchStats, chart as fetchChart } from "../api/usage";
import { Card } from "../components/Card";

function mapChartPoints(payload) {
  const raw = payload?.data ?? payload?.points ?? (Array.isArray(payload) ? payload : []);
  if (!Array.isArray(raw)) return [];
  return raw.map((row) => ({
    date: row.date ?? row.Date ?? row.day ?? "",
    requests: Number(row.requests ?? row.Requests ?? 0),
    promptTokens: Number(row.promptTokens ?? row.PromptTokens ?? 0),
    completionTokens: Number(row.completionTokens ?? row.CompletionTokens ?? 0),
  }));
}

export default function UsagePage() {
  const [summary, setSummary] = useState(null);
  const [points, setPoints] = useState([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    (async () => {
      setLoading(true);
      setError("");
      try {
        const [s, c] = await Promise.all([fetchStats(), fetchChart()]);
        if (!alive) return;
        setSummary(s);
        setPoints(mapChartPoints(c));
      } catch (err) {
        if (alive) setError(err?.message || String(err));
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  const requests = summary?.requests ?? 0;
  const prompt = summary?.promptTokens ?? summary?.prompt_tokens ?? 0;
  const completion = summary?.completionTokens ?? summary?.completion_tokens ?? 0;
  const totalTokens = Number(prompt) + Number(completion);

  return (
    <div className="space-y-6">
      <h1 className="text-lg font-semibold text-[var(--text)]">Usage</h1>
      {loading ? <p className="text-sm text-[var(--muted)]">Loading…</p> : null}
      {error ? <p className="text-sm text-[var(--danger)]">{error}</p> : null}

      {!loading && !error ? (
        <>
          <div className="grid gap-3 sm:grid-cols-3">
            <Card className="p-4">
              <p className="text-xs uppercase tracking-wide text-[var(--muted)]">Requests</p>
              <p className="mt-1 text-xl font-semibold tabular-nums text-[var(--text)]">{requests}</p>
            </Card>
            <Card className="p-4">
              <p className="text-xs uppercase tracking-wide text-[var(--muted)]">Prompt tokens</p>
              <p className="mt-1 text-xl font-semibold tabular-nums text-[var(--text)]">{prompt}</p>
            </Card>
            <Card className="p-4">
              <p className="text-xs uppercase tracking-wide text-[var(--muted)]">Completion tokens</p>
              <p className="mt-1 text-xl font-semibold tabular-nums text-[var(--text)]">{completion}</p>
            </Card>
          </div>

          <Card className="p-4">
            <p className="mb-3 text-xs uppercase tracking-wide text-[var(--muted)]">
              Daily requests
              {totalTokens ? ` · ${totalTokens} tokens total` : ""}
            </p>
            {points.length === 0 ? (
              <p className="py-12 text-center text-sm text-[var(--muted)]">No usage data yet.</p>
            ) : (
              <div className="h-64 w-full">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={points}>
                    <CartesianGrid stroke="#423d3a" strokeDasharray="3 3" />
                    <XAxis dataKey="date" stroke="#756861" tick={{ fontSize: 11 }} />
                    <YAxis stroke="#756861" tick={{ fontSize: 11 }} allowDecimals={false} />
                    <Tooltip
                      contentStyle={{
                        background: "#262626",
                        border: "1px solid rgba(255,245,235,0.10)",
                        borderRadius: 6,
                        fontSize: 12,
                      }}
                    />
                    <Line
                      type="monotone"
                      dataKey="requests"
                      stroke="#E56A4A"
                      strokeWidth={2}
                      dot={false}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            )}
          </Card>
        </>
      ) : null}
    </div>
  );
}
