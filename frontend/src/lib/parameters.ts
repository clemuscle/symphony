// Formulaire des paramètres d'une stack (spec.parameters). L'API valide les
// valeurs avec le JSON Schema de la stack : ici, l'affichage (conditions,
// défauts) et un premier contrôle pour guider la saisie.
import type { ParamValue, StackParameter } from '../types'

export type ParamValues = Record<string, ParamValue>

// matches : la valeur got remplit-elle la condition want (une valeur, ou
// l'une d'une liste) ?
function matches(got: ParamValue | undefined, want: ParamValue | ParamValue[]): boolean {
  if (got === undefined) return false
  return Array.isArray(want) ? want.includes(got) : got === want
}

// visible : le paramètre existe-t-il avec ces valeurs (sa condition when) ?
export function visible(p: StackParameter, values: ParamValues): boolean {
  return Object.entries(p.when || {}).every(([name, want]) => matches(values[name], want))
}

// fillDefaults : les valeurs complétées par les défauts des paramètres qui
// existent, dans l'ordre (une condition porte sur un paramètre déclaré
// avant) ; ceux qui n'existent plus sont retirés
export function fillDefaults(params: StackParameter[], values: ParamValues): ParamValues {
  const out: ParamValues = {}
  for (const p of params) {
    if (!visible(p, out)) continue
    if (p.locked && p.default !== undefined) out[p.name] = p.default
    else if (values[p.name] !== undefined) out[p.name] = values[p.name]
    else if (p.default !== undefined) out[p.name] = p.default
  }
  return out
}

// paramErrors : paramètre → message, pour les champs requis vides ou les
// nombres invalides
export function paramErrors(params: StackParameter[], values: ParamValues): Record<string, string> {
  const errors: Record<string, string> = {}
  for (const p of params) {
    if (!visible(p, values)) continue
    const v = values[p.name]
    if (p.type === 'number' && v !== undefined && (typeof v !== 'number' || Number.isNaN(v))) {
      errors[p.name] = 'Nombre attendu'
    } else if (p.required && (v === undefined || v === '')) {
      errors[p.name] = 'Requis'
    }
  }
  return errors
}

// formatValue : une valeur pour l'affichage
export function formatValue(v: unknown): string {
  if (v === true) return 'oui'
  if (v === false) return 'non'
  return v === undefined || v === null ? '' : String(v)
}
