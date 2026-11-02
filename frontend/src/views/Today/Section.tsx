/** A titled section of a screen, with an optional way to see all of it. */
export const Section = ({ id, title, count, more, children }: {
  id: string; title: string; count?: number | null; more?: { label: string; go: () => void }; children: React.ReactNode;
}) => (
  <section className="mt-7" aria-labelledby={id}>
    <header className="mb-2 flex items-baseline gap-3">
      <h2 id={id} className="flex-1 text-lg font-semibold">
        {title}{count != null && <span className="font-normal text-muted-foreground"> · {count}</span>}
      </h2>
      {more && <button type="button" className="text-sm underline underline-offset-2" onClick={more.go}>{more.label}</button>}
    </header>
    {children}
  </section>
);

