// ESLint : règles recommandées de JavaScript, TypeScript et Vue
import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['node_modules', 'dist', 'dist-visit'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended'],
  {
    files: ['**/*.vue'],
    languageOptions: { parserOptions: { parser: tseslint.parser } },
  },
  {
    rules: {
      // Variables globales (window, setTimeout…) : TypeScript les vérifie déjà
      'no-undef': 'off',
      // Mise en forme des gabarits : compacte, un attribut par ligne alourdirait
      // les vues ; ces règles ne protègent d'aucun bogue
      'vue/max-attributes-per-line': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/multiline-html-element-content-newline': 'off',
      'vue/html-self-closing': 'off',
      'vue/attributes-order': 'off',
      'vue/html-indent': 'off',
      'vue/first-attribute-linebreak': 'off',
      'vue/html-closing-bracket-newline': 'off',
    },
  },
  {
    // Les vues sont des pages, nommées par le routeur
    files: ['src/views/*.vue', 'src/App.vue'],
    rules: { 'vue/multi-word-component-names': 'off' },
  },
)
