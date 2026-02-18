import Link from "next/link";
import type { ReactNode } from "react";

interface CardProps {
  title: string;
  href?: string;
  icon?: ReactNode;
  arrow?: boolean;
  children?: ReactNode;
}

export function Card({ title, href, icon, arrow, children }: CardProps) {
  const content = (
    <>
      {children && <div className="card-body">{children}</div>}
      <span className="card-title">
        {icon}
        <span>{title}</span>
        {arrow && <span className="card-arrow">{"\u2192"}</span>}
      </span>
    </>
  );

  if (href) {
    return (
      <Link href={href} className="brutal-card card-link">
        {content}
      </Link>
    );
  }

  return <div className="brutal-card card-static">{content}</div>;
}

interface CardsProps {
  children: ReactNode;
  num?: number;
}

export function Cards({ children, num = 3 }: CardsProps) {
  return (
    <div
      className="cards-grid"
      style={{ ["--cards-cols" as string]: num }}
    >
      {children}
    </div>
  );
}
