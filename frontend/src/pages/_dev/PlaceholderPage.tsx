import { Card } from "@/components/primitives/Card";

interface PlaceholderPageProps {
  title: string;
  body?: string;
  children?: React.ReactNode;
}

export function PlaceholderPage({ title, body, children }: PlaceholderPageProps) {
  return (
    <div className="mx-auto max-w-container-content page-x py-10">
      <header className="flex flex-col gap-2">
        <h1 className="text-section-sm font-semibold tracking-section text-ink md:text-section">
          {title}
        </h1>
        {body && (
          <p className="max-w-container-small text-[15px] tracking-sub text-ink-muted">{body}</p>
        )}
      </header>
      {children && <Card className="mt-8 p-6">{children}</Card>}
    </div>
  );
}
