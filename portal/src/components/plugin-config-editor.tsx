"use client";

import { useState, useEffect, useCallback, useMemo } from "react";
import { useUpdatePluginConfig } from "@/hooks/use-api";
import { useScreenGlitch } from "@/components/screen-glitch";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Save, Loader2, ChevronDown, ChevronRight, Plus, Trash2 } from "lucide-react";

interface JSONSchema {
  type?: string;
  properties?: Record<string, JSONSchema>;
  items?: JSONSchema;
  enum?: string[];
  default?: unknown;
  description?: string;
  title?: string;
  required?: string[];
}

interface PluginConfigEditorProps {
  name: string;
  descriptor?: Record<string, unknown>;
  currentConfig: Record<string, unknown>;
}

export function PluginConfigEditor({
  name,
  descriptor,
  currentConfig,
}: PluginConfigEditorProps) {
  const { updateConfig, updating, error } = useUpdatePluginConfig();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();

  // Parse schema from descriptor
  const schema = useMemo<JSONSchema | null>(() => {
    const raw = descriptor?.config_schema_json;
    if (!raw) return null;
    try {
      if (typeof raw === "string") return JSON.parse(raw) as JSONSchema;
      if (typeof raw === "object") return raw as JSONSchema;
    } catch {
      // Parse error — fallback to raw JSON editor
    }
    return null;
  }, [descriptor]);

  const [formState, setFormState] = useState<Record<string, unknown>>({});
  const [rawJson, setRawJson] = useState("");
  const [jsonError, setJsonError] = useState<string | null>(null);

  // Initialize form state from current config + schema defaults
  useEffect(() => {
    if (schema?.properties) {
      const initial: Record<string, unknown> = {};
      for (const [key, prop] of Object.entries(schema.properties)) {
        initial[key] =
          currentConfig[key] !== undefined
            ? currentConfig[key]
            : prop.default !== undefined
              ? prop.default
              : getDefaultForType(prop.type);
      }
      setFormState(initial);
    } else {
      setRawJson(JSON.stringify(currentConfig, null, 2));
    }
  }, [currentConfig, schema]);

  // Dirty check
  const isDirty = useMemo(() => {
    if (schema?.properties) {
      return JSON.stringify(formState) !== JSON.stringify(currentConfig);
    }
    try {
      return JSON.stringify(JSON.parse(rawJson)) !== JSON.stringify(currentConfig);
    } catch {
      return rawJson !== JSON.stringify(currentConfig, null, 2);
    }
  }, [formState, rawJson, currentConfig, schema]);

  const handleSave = useCallback(async () => {
    let config: Record<string, unknown>;
    if (schema?.properties) {
      config = formState;
    } else {
      try {
        config = JSON.parse(rawJson);
        setJsonError(null);
      } catch (e) {
        setJsonError(e instanceof Error ? e.message : "Invalid JSON");
        return;
      }
    }
    try {
      await updateConfig(name, config);
    } catch {
      triggerGlitch();
    }
  }, [name, formState, rawJson, schema, updateConfig, triggerGlitch]);

  // No config at all
  if (!descriptor?.config_schema_json && Object.keys(currentConfig).length === 0) {
    return (
      <div className="text-[10px] text-muted-foreground">
        No configuration available for this plugin.
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <GlitchOverlay />
      <h4 className="text-[10px] font-bold tracking-wider text-muted-foreground">
        Configuration
      </h4>

      {schema?.properties ? (
        <SchemaForm
          schema={schema}
          value={formState}
          onChange={setFormState}
          path=""
        />
      ) : (
        <div className="space-y-1">
          <Textarea
            value={rawJson}
            onChange={(e) => {
              setRawJson(e.target.value);
              setJsonError(null);
            }}
            className="font-mono text-[10px] min-h-[120px]"
            placeholder="{}"
          />
          {jsonError && (
            <p className="text-[10px] text-destructive">{jsonError}</p>
          )}
        </div>
      )}

      {error && (
        <p className="text-[10px] text-destructive">{error.message}</p>
      )}

      <Button
        size="sm"
        onClick={handleSave}
        disabled={!isDirty || updating}
        className="text-[10px] h-7"
      >
        {updating ? (
          <Loader2 className="h-3 w-3 animate-spin mr-1" />
        ) : (
          <Save className="h-3 w-3 mr-1" />
        )}
        Save Config
      </Button>
    </div>
  );
}

// ── Schema-driven form renderer ────────────────────────────────────────────

function SchemaForm({
  schema,
  value,
  onChange,
  path,
}: {
  schema: JSONSchema;
  value: Record<string, unknown>;
  onChange: (v: Record<string, unknown>) => void;
  path: string;
}) {
  if (!schema.properties) return null;

  return (
    <div className="space-y-2">
      {Object.entries(schema.properties).map(([key, prop]) => (
        <SchemaField
          key={key}
          name={key}
          schema={prop}
          value={value[key]}
          onChange={(v) => onChange({ ...value, [key]: v })}
          path={path ? `${path}.${key}` : key}
        />
      ))}
    </div>
  );
}

function SchemaField({
  name,
  schema,
  value,
  onChange,
  path,
}: {
  name: string;
  schema: JSONSchema;
  value: unknown;
  onChange: (v: unknown) => void;
  path: string;
}) {
  const fieldId = `config-${path}`;
  const label = schema.title ?? name;

  // String enum → Select
  if (schema.type === "string" && schema.enum) {
    return (
      <div className="space-y-1">
        <Label htmlFor={fieldId} className="text-[10px] tracking-wider">
          {label}
        </Label>
        {schema.description && (
          <p className="text-[10px] text-muted-foreground">{schema.description}</p>
        )}
        <Select value={(value as string) ?? ""} onValueChange={onChange}>
          <SelectTrigger className="text-xs h-8">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {schema.enum.map((opt) => (
              <SelectItem key={opt} value={opt} className="text-xs">
                {opt}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    );
  }

  // String → text input
  if (schema.type === "string") {
    return (
      <div className="space-y-1">
        <Label htmlFor={fieldId} className="text-[10px] tracking-wider">
          {label}
        </Label>
        {schema.description && (
          <p className="text-[10px] text-muted-foreground">{schema.description}</p>
        )}
        <Input
          id={fieldId}
          type="text"
          value={(value as string) ?? ""}
          onChange={(e) => onChange(e.target.value)}
          className="text-xs h-8 font-mono"
        />
      </div>
    );
  }

  // Number / Integer → number input
  if (schema.type === "number" || schema.type === "integer") {
    return (
      <div className="space-y-1">
        <Label htmlFor={fieldId} className="text-[10px] tracking-wider">
          {label}
        </Label>
        {schema.description && (
          <p className="text-[10px] text-muted-foreground">{schema.description}</p>
        )}
        <Input
          id={fieldId}
          type="number"
          value={value !== undefined && value !== null ? String(value) : ""}
          onChange={(e) => {
            const n = Number(e.target.value);
            onChange(Number.isNaN(n) ? 0 : n);
          }}
          className="text-xs h-8 font-mono w-32"
        />
      </div>
    );
  }

  // Boolean → toggle button group
  if (schema.type === "boolean") {
    const boolVal = value === true;
    return (
      <div className="space-y-1">
        <Label className="text-[10px] tracking-wider">{label}</Label>
        {schema.description && (
          <p className="text-[10px] text-muted-foreground">{schema.description}</p>
        )}
        <div className="flex gap-1">
          <Button
            size="sm"
            variant={boolVal ? "default" : "ghost"}
            className="text-[10px] h-6 px-2"
            onClick={() => onChange(true)}
          >
            On
          </Button>
          <Button
            size="sm"
            variant={!boolVal ? "default" : "ghost"}
            className="text-[10px] h-6 px-2"
            onClick={() => onChange(false)}
          >
            Off
          </Button>
        </div>
      </div>
    );
  }

  // Object (nested) → collapsible fieldset
  if (schema.type === "object" && schema.properties) {
    return (
      <CollapsibleFieldset label={label} description={schema.description}>
        <SchemaForm
          schema={schema}
          value={(value as Record<string, unknown>) ?? {}}
          onChange={(v) => onChange(v)}
          path={path}
        />
      </CollapsibleFieldset>
    );
  }

  // Array → add/remove items
  if (schema.type === "array" && schema.items) {
    return (
      <ArrayField
        name={name}
        label={label}
        description={schema.description}
        itemSchema={schema.items}
        value={(value as unknown[]) ?? []}
        onChange={onChange}
        path={path}
      />
    );
  }

  // Fallback: raw JSON
  return (
    <FallbackField
      name={name}
      label={label}
      description={schema.description}
      value={value}
      onChange={onChange}
    />
  );
}

// ── Collapsible fieldset ───────────────────────────────────────────────────

function CollapsibleFieldset({
  label,
  description,
  children,
}: {
  label: string;
  description?: string;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="border border-border p-2 space-y-2">
      <button
        type="button"
        className="flex items-center gap-1 text-[10px] font-bold tracking-wider text-muted-foreground"
        onClick={() => setOpen(!open)}
      >
        {open ? (
          <ChevronDown className="h-3 w-3" />
        ) : (
          <ChevronRight className="h-3 w-3" />
        )}
        {label}
      </button>
      {description && !open && (
        <p className="text-[10px] text-muted-foreground pl-4">{description}</p>
      )}
      {open && <div className="pl-4">{children}</div>}
    </div>
  );
}

// ── Array field ────────────────────────────────────────────────────────────

function ArrayField({
  name,
  label,
  description,
  itemSchema,
  value,
  onChange,
  path,
}: {
  name: string;
  label: string;
  description?: string;
  itemSchema: JSONSchema;
  value: unknown[];
  onChange: (v: unknown) => void;
  path: string;
}) {
  const addItem = () => {
    onChange([...value, getDefaultForType(itemSchema.type)]);
  };

  const removeItem = (idx: number) => {
    onChange(value.filter((_, i) => i !== idx));
  };

  const updateItem = (idx: number, v: unknown) => {
    const next = [...value];
    next[idx] = v;
    onChange(next);
  };

  return (
    <div className="space-y-1">
      <Label className="text-[10px] tracking-wider">{label}</Label>
      {description && (
        <p className="text-[10px] text-muted-foreground">{description}</p>
      )}
      <div className="space-y-1 pl-2 border-l border-border">
        {value.map((item, idx) => (
          <div key={idx} className="flex items-start gap-1">
            <div className="flex-1">
              {itemSchema.type === "object" && itemSchema.properties ? (
                <SchemaForm
                  schema={itemSchema}
                  value={(item as Record<string, unknown>) ?? {}}
                  onChange={(v) => updateItem(idx, v)}
                  path={`${path}[${idx}]`}
                />
              ) : (
                <SchemaField
                  name={`${name}[${idx}]`}
                  schema={itemSchema}
                  value={item}
                  onChange={(v) => updateItem(idx, v)}
                  path={`${path}[${idx}]`}
                />
              )}
            </div>
            <Button
              variant="ghost"
              size="icon"
              className="h-6 w-6 text-destructive hover:text-destructive shrink-0"
              onClick={() => removeItem(idx)}
            >
              <Trash2 className="h-3 w-3" />
            </Button>
          </div>
        ))}
      </div>
      <Button
        variant="ghost"
        size="sm"
        className="text-[10px] h-6"
        onClick={addItem}
      >
        <Plus className="h-3 w-3 mr-1" />
        Add item
      </Button>
    </div>
  );
}

// ── Fallback raw JSON field ────────────────────────────────────────────────

function FallbackField({
  name,
  label,
  description,
  value,
  onChange,
}: {
  name: string;
  label: string;
  description?: string;
  value: unknown;
  onChange: (v: unknown) => void;
}) {
  const [text, setText] = useState(JSON.stringify(value ?? null, null, 2));
  const [parseError, setParseError] = useState<string | null>(null);

  return (
    <div className="space-y-1">
      <Label className="text-[10px] tracking-wider">
        {label}{" "}
        <span className="text-muted-foreground font-normal">(JSON)</span>
      </Label>
      {description && (
        <p className="text-[10px] text-muted-foreground">{description}</p>
      )}
      <Textarea
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          try {
            onChange(JSON.parse(e.target.value));
            setParseError(null);
          } catch (err) {
            setParseError(
              err instanceof Error ? err.message : "Invalid JSON",
            );
          }
        }}
        className="font-mono text-[10px] min-h-[60px]"
      />
      {parseError && (
        <p className="text-[10px] text-destructive">{parseError}</p>
      )}
    </div>
  );
}

// ── Helpers ────────────────────────────────────────────────────────────────

function getDefaultForType(type?: string): unknown {
  switch (type) {
    case "string":
      return "";
    case "number":
    case "integer":
      return 0;
    case "boolean":
      return false;
    case "object":
      return {};
    case "array":
      return [];
    default:
      return null;
  }
}
