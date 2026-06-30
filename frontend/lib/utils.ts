import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatEvidenceID(id: string): string {
  const match = id.match(/^(?:EV?)?0*(\d+)$/i)
  if (match) {
    return `证据${match[1]}`
  }
  return id
}
