import { describe, expect, it } from 'vitest'
import { icon, runtimeKind, runtimeLabel } from './icons'

describe('icon', () => {
  it('un logo pour un langage ou un outil, en couleur lisible', () => {
    const go = icon('Go')
    expect('brand' in go && go.brand.title).toBe('Go')
    const vault = icon('vault')
    expect('brand' in vault && vault.brand.hex).toBe('000000')
  })
  it('un pictogramme sinon, et le repli pour un nom inconnu', () => {
    expect('picto' in icon('docs')).toBe(true)
    expect(icon('inconnu')).toEqual(icon('package'))
  })
})

describe('runtimeKind', () => {
  it("devine la technologie d'après l'id", () => {
    expect(runtimeKind('argocd-k3s')).toBe('kubernetes')
    expect(runtimeKind('docker-local')).toBe('docker')
    expect(runtimeKind('nomad')).toBe('')
    expect(runtimeLabel('nomad')).toBe('nomad')
    expect(runtimeLabel('docker')).toBe('Docker')
  })
})
