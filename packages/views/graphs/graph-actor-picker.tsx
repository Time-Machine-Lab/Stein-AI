"use client";

import { useState } from "react";
import { Bot, ChevronDown, UserRound } from "lucide-react";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import {
  PickerEmpty,
  PickerItem,
  PropertyPicker,
} from "../issues/components/pickers/property-picker";
import { matchesPinyin } from "../editor/extensions/pinyin-match";

export interface GraphActorOption {
  value: string;
  label: string;
  avatarUrl?: string | null;
}

/** Shares the searchable picker interaction used by tasks and automations. */
export function GraphActorPicker({
  id,
  label,
  value,
  onChange,
  options,
  placeholder,
  disabled,
  isAgent = false,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: GraphActorOption[];
  placeholder: string;
  disabled?: boolean;
  isAgent?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const selected = options.find((option) => option.value === value);
  const query = search.trim().toLowerCase();
  const filtered = options.filter(
    (option) =>
      !query ||
      option.label.toLowerCase().includes(query) ||
      matchesPinyin(option.label, query),
  );
  const PlaceholderIcon = isAgent ? Bot : UserRound;
  const avatar = (option: GraphActorOption) => (
    <span aria-hidden="true">
      <ActorAvatar
        name={option.label}
        initials={Array.from(option.label).slice(0, 2).join("")}
        avatarUrl={option.avatarUrl}
        isAgent={isAgent}
        size="sm"
      />
    </span>
  );
  return (
    <PropertyPicker
      open={open && !disabled}
      onOpenChange={(next) => {
        if (!disabled) setOpen(next);
      }}
      align="start"
      width="w-72 max-w-[calc(100vw-2rem)]"
      searchable
      searchPlaceholder={placeholder}
      onSearchChange={setSearch}
      triggerRender={
        <Button
          id={id}
          type="button"
          role="combobox"
          aria-label={label}
          aria-expanded={open && !disabled}
          aria-required="true"
          variant="outline"
          disabled={disabled}
          className="h-9 w-full min-w-0 justify-start font-normal"
        />
      }
      trigger={
        <>
          {selected ? (
            avatar(selected)
          ) : (
            <PlaceholderIcon className="size-4 text-muted-foreground" />
          )}
          <span className="min-w-0 flex-1 truncate text-left">
            {selected?.label ?? (value || placeholder)}
          </span>
          <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
        </>
      }
    >
      {filtered.length ? (
        filtered.map((option) => (
          <PickerItem
            key={option.value}
            selected={option.value === value}
            onClick={() => {
              onChange(option.value);
              setOpen(false);
            }}
          >
            {avatar(option)}
            <span className="truncate">{option.label}</span>
          </PickerItem>
        ))
      ) : (
        <PickerEmpty />
      )}
    </PropertyPicker>
  );
}
