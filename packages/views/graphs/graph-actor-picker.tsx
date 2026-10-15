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

const actorAvatar = (option: GraphActorOption, isAgent: boolean) => (
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

const filterActorOptions = (options: GraphActorOption[], search: string) => {
  const query = search.trim().toLowerCase();
  return options.filter(
    (option) =>
      !query ||
      option.label.toLowerCase().includes(query) ||
      matchesPinyin(option.label, query),
  );
};

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
  const filtered = filterActorOptions(options, search);
  const PlaceholderIcon = isAgent ? Bot : UserRound;
  const avatar = (option: GraphActorOption) => actorAvatar(option, isAgent);
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

/**
 * Multi-select sibling of `GraphActorPicker` rather than a mode on it: the
 * single-select picker is used by the node inspector too, and a mode flag would
 * turn its `onChange` polymorphic for every call site.
 *
 * Toggling keeps the popover open so a scope can be assembled in one visit. The
 * trigger is a button, so chips carry no remove affordance of their own —
 * deselecting in the list is how an actor leaves the scope.
 */
export function GraphActorMultiPicker({
  id,
  label,
  values,
  onChange,
  options,
  placeholder,
  disabled,
  isAgent = false,
}: {
  id: string;
  label: string;
  values: string[];
  onChange: (values: string[]) => void;
  options: GraphActorOption[];
  placeholder: string;
  disabled?: boolean;
  isAgent?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  // Resolve in the caller's order and drop ids whose actor is gone, the way
  // the draft-mode label picker does.
  const selected = values
    .map((value) => options.find((option) => option.value === value))
    .filter((option): option is GraphActorOption => Boolean(option));
  const filtered = filterActorOptions(options, search);
  const PlaceholderIcon = isAgent ? Bot : UserRound;
  const avatar = (option: GraphActorOption) => actorAvatar(option, isAgent);
  const toggle = (value: string) =>
    onChange(
      values.includes(value)
        ? values.filter((current) => current !== value)
        : [...values, value],
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
          variant="outline"
          disabled={disabled}
          className="h-auto min-h-9 w-full min-w-0 justify-start py-1.5 font-normal"
        />
      }
      trigger={
        <>
          <span className="flex min-w-0 flex-1 flex-wrap items-center gap-1 text-left">
            {selected.length ? (
              selected.map((option) => (
                <span
                  key={option.value}
                  className="flex min-w-0 items-center gap-1 rounded-full bg-muted px-2 py-0.5"
                >
                  {avatar(option)}
                  <span className="max-w-32 truncate text-caption">
                    {option.label}
                  </span>
                </span>
              ))
            ) : (
              <>
                <PlaceholderIcon className="size-4 text-muted-foreground" />
                <span className="truncate text-muted-foreground">
                  {placeholder}
                </span>
              </>
            )}
          </span>
          <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
        </>
      }
    >
      {filtered.length ? (
        filtered.map((option) => (
          <PickerItem
            key={option.value}
            selected={values.includes(option.value)}
            onClick={() => toggle(option.value)}
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
