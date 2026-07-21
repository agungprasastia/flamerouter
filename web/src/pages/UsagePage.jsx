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

function Card({ label, value }) {
  return (
    <div className="rounded border border-slate-800 bg-[#0d1219] px-4 py-3">
      <p className="text-xs uppercase tracking-wide text-slate-400">{label}</p>
      <p className="mt-1 text-xl font-semibold tabular-nums text-slate-100">{value}</p>
    </div>
  );
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
    <div className="p-6">
      <h1 className="mb-4 text-lg font-semibold text-slate-100">Usage</h1>
      {loading ? <p className="text-sm text-slate-400">Loading…</p> : null}
      {error ? <p className="mb-3 text-sm text-red-400">{error}</p> : null}

      {!loading && !error ? (
        <>
          <div className="mb-6 grid gap-3 sm:grid-cols-3">
            <Card label="Requests" value={requests} />
            <Card label="Prompt tokens" value={prompt} />
            <Card label="Completion tokens" value={completion} />
          </div>

          <div className="rounded border border-slate-800 bg-[#0d1219] p-4">
            <p className="mb-3 text-xs uppercase tracking-wide text-slate-400">
              Daily requests
              {totalTokens ? ` · ${totalTokens} tokens total` : ""}
            </p>
            {points.length === 0 ? (
              <p className="py-12 text-center text-sm text-slate-400">No usage data yet.</p>
            ) : (
              <div className="h-64 w-full">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={points}>
                    <CartesianGrid stroke="#1e293b" strokeDasharray="3 3" />
                    <XAxis dataKey="date" stroke="#94a3b8" tick={{ fontSize: 11 }} />
                    <YAxis stroke="#94a3b8" tick={{ fontSize: 11 }} allowDecimals={false} />
                    <Tooltip
                      contentStyle={{
                        background: "#0d1219",
                        border: "1px solid #1e293b",
                        borderRadius: 6,
                        fontSize: 12,
                      }}
                    />
                    <Line
                      type="monotone"
                      dataKey="requests"
                      stroke="#38bdf8"
                      strokeWidth={2}
                      dot={false}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            )}
          </div>
        </>
      ) : null}
    </div>
  );
}
