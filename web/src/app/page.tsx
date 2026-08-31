export default function Home() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-6 px-6 text-center">
      <div className="rounded-2xl bg-gradient-to-r from-indigo-500 to-violet-500 p-[1px]">
        <div className="rounded-2xl bg-[#0b0d14] px-8 py-6">
          <h1 className="text-4xl font-bold tracking-tight">
            Jom<span className="text-indigo-400">Form</span>
          </h1>
          <p className="mt-3 text-slate-400">
            Borang jualan &amp; sistem ejen anda — siap dalam 5 minit.
          </p>
        </div>
      </div>
      <p className="text-sm text-slate-500">
        Dashboard sedang dibina. Sementara itu, API sudah hidup di{" "}
        <code className="rounded bg-slate-800 px-1.5 py-0.5 text-emerald-400">
          /healthz
        </code>
      </p>
    </main>
  );
}