"use client";

import { useState, useCallback, useEffect } from "react";
import { useIdentityUsers, useIdentityCapabilities } from "@/hooks/use-api";
import { Loading } from "@/components/loading";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { ArrowLeft, Search, ChevronLeft, ChevronRight, Users } from "lucide-react";
import Link from "next/link";

const PAGE_SIZE = 20;

export function UserListClient() {
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [offset, setOffset] = useState(0);

  const { data: capabilities } = useIdentityCapabilities();
  const { data, isLoading, error } = useIdentityUsers({
    search: debouncedSearch || undefined,
    limit: PAGE_SIZE,
    offset,
  });

  // Debounce search input
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(search);
      setOffset(0); // reset to first page on search
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);

  const handlePrev = useCallback(() => {
    setOffset((prev) => Math.max(0, prev - PAGE_SIZE));
  }, []);

  const handleNext = useCallback(() => {
    if (data && offset + PAGE_SIZE < data.total_count) {
      setOffset((prev) => prev + PAGE_SIZE);
    }
  }, [data, offset]);

  const totalPages = data ? Math.ceil(data.total_count / PAGE_SIZE) : 0;
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1;

  return (
    <div className="space-y-6 max-w-5xl">
      {/* Header */}
      <div>
        <Link
          href="/settings"
          className="text-[10px] text-muted-foreground hover:text-primary flex items-center gap-1 mb-2"
        >
          <ArrowLeft className="h-3 w-3" /> Back to settings
        </Link>
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-lg font-bold tracking-wider gradient-text">
              User Management
            </h1>
            {capabilities && (
              <p className="text-[10px] text-muted-foreground mt-1">
                Provider: <span className="text-primary font-mono">{capabilities.provider_name}</span>
                {data && (
                  <span className="ml-2">
                    {data.total_count} user{data.total_count !== 1 ? "s" : ""}
                  </span>
                )}
              </p>
            )}
          </div>
        </div>
      </div>

      {/* Search */}
      <div className="brutal-card p-3">
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
          <Input
            placeholder="Search users by name or email..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-9 h-8 text-xs bg-background border-border"
          />
        </div>
      </div>

      {/* Error state */}
      {error && (
        <div className="brutal-card p-4 border-destructive">
          <p className="text-xs text-destructive">Failed to load users: {error.message}</p>
        </div>
      )}

      {/* Loading state */}
      {isLoading && <Loading text="Loading users..." />}

      {/* User table */}
      {data && !isLoading && (
        <div className="brutal-card overflow-hidden">
          {data.users.length === 0 ? (
            <div className="p-8 text-center">
              <Users className="h-8 w-8 mx-auto text-muted-foreground mb-2" />
              <p className="text-sm text-muted-foreground">
                {debouncedSearch ? "No users match your search." : "No users found."}
              </p>
            </div>
          ) : (
            <>
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="border-b border-border bg-muted/30">
                      <th className="text-left px-4 py-2.5 font-medium tracking-wider text-muted-foreground">#</th>
                      <th className="text-left px-4 py-2.5 font-medium tracking-wider text-muted-foreground">Display Name</th>
                      <th className="text-left px-4 py-2.5 font-medium tracking-wider text-muted-foreground">Email</th>
                      <th className="text-left px-4 py-2.5 font-medium tracking-wider text-muted-foreground">Status</th>
                      <th className="text-left px-4 py-2.5 font-medium tracking-wider text-muted-foreground">Groups</th>
                      <th className="text-left px-4 py-2.5 font-medium tracking-wider text-muted-foreground">Created</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.users.map((user, i) => (
                      <tr
                        key={user.id}
                        className={`border-b border-border/50 hover:bg-muted/20 transition-colors ${
                          i % 2 === 0 ? "bg-background" : "bg-muted/10"
                        }`}
                      >
                        <td className="px-4 py-2.5 text-muted-foreground tabular-nums">
                          {offset + i + 1}
                        </td>
                        <td className="px-4 py-2.5 font-medium">
                          {user.display_name}
                        </td>
                        <td className="px-4 py-2.5 text-muted-foreground font-mono">
                          {user.email}
                        </td>
                        <td className="px-4 py-2.5">
                          <Badge
                            variant={user.enabled ? "default" : "destructive"}
                            className="text-[10px] px-1.5 py-0"
                          >
                            {user.enabled ? "Active" : "Disabled"}
                          </Badge>
                        </td>
                        <td className="px-4 py-2.5">
                          <div className="flex flex-wrap gap-1">
                            {user.groups?.length > 0 ? (
                              user.groups.map((g) => (
                                <Badge
                                  key={g.id}
                                  variant="outline"
                                  className="text-[10px] px-1.5 py-0 font-mono"
                                >
                                  {g.name}
                                </Badge>
                              ))
                            ) : (
                              <span className="text-muted-foreground">-</span>
                            )}
                          </div>
                        </td>
                        <td className="px-4 py-2.5 text-muted-foreground tabular-nums">
                          {user.created_at
                            ? new Date(user.created_at).toLocaleDateString()
                            : "-"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              {/* Pagination */}
              {totalPages > 1 && (
                <div className="flex items-center justify-between px-4 py-2.5 border-t border-border bg-muted/20">
                  <span className="text-[10px] text-muted-foreground">
                    Page {currentPage} of {totalPages}
                  </span>
                  <div className="flex items-center gap-1">
                    <button
                      onClick={handlePrev}
                      disabled={offset === 0}
                      className="p-1 text-muted-foreground hover:text-foreground disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                    >
                      <ChevronLeft className="h-4 w-4" />
                    </button>
                    <button
                      onClick={handleNext}
                      disabled={!data || offset + PAGE_SIZE >= data.total_count}
                      className="p-1 text-muted-foreground hover:text-foreground disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                    >
                      <ChevronRight className="h-4 w-4" />
                    </button>
                  </div>
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
