export function graphUtf8Length(value: string): number {
  return Array.from(value).reduce((bytes, character) => {
    const point = character.codePointAt(0)!;
    return (
      bytes + (point <= 0x7f ? 1 : point <= 0x7ff ? 2 : point <= 0xffff ? 3 : 4)
    );
  }, 0);
}
