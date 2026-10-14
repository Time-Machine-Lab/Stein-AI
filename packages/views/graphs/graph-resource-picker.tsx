"use client";
import { useState } from "react";
import { ChevronDown, Loader2, Search } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  PropertyPicker,
  PickerEmpty,
  PickerItem,
} from "../issues/components/pickers/property-picker";

export function GraphResourcePicker({
  id,
  label,
  value,
  options,
  onChange,
  placeholder,
  disabled,
  search,
  onSearch,
  loading,
}: {
  id: string;
  label: string;
  value: string;
  options: { value: string; label: string; description?: string }[];
  onChange: (value: string) => void;
  placeholder: string;
  disabled?: boolean;
  search?: string;
  onSearch?: (query: string) => void;
  loading?: boolean;
}) {
  const [open, setOpen] = useState(false),
    [localSearch, setLocalSearch] = useState("");
  const selected = options.find((option) => option.value === value),
    query = (search ?? localSearch).toLowerCase();
  const filtered = onSearch
    ? options
    : options.filter((item) =>
        `${item.label} ${item.description ?? ""}`.toLowerCase().includes(query),
      );
  return (
    <PropertyPicker
      open={open && !disabled}
      onOpenChange={setOpen}
      align="start"
      width="w-[420px] max-w-[calc(100vw-2rem)]"
      searchable
      searchPlaceholder={placeholder}
      onSearchChange={onSearch ?? setLocalSearch}
      triggerRender={
        <Button
          id={id}
          type="button"
          role="combobox"
          aria-label={label}
          aria-expanded={open}
          variant="outline"
          disabled={disabled}
          className="w-full min-w-0 justify-start font-normal"
        />
      }
      trigger={
        <>
          <Search className="size-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate text-left">
            {selected?.label ?? (value || placeholder)}
          </span>
          <ChevronDown className="size-3.5 shrink-0" />
        </>
      }
    >
      {loading && (
        <div role="status" className="flex justify-center p-3">
          <Loader2 className="size-4 animate-spin text-muted-foreground" />
        </div>
      )}
      {filtered.map((option) => (
        <PickerItem
          key={option.value}
          selected={value === option.value}
          onClick={() => {
            onChange(option.value);
            setOpen(false);
          }}
        >
          <div className="min-w-0">
            <p className="truncate text-body">{option.label}</p>
            {option.description && (
              <p className="mt-0.5 truncate text-caption text-muted-foreground">
                {option.description}
              </p>
            )}
          </div>
        </PickerItem>
      ))}
      {!loading && !filtered.length && <PickerEmpty />}
    </PropertyPicker>
  );
}
