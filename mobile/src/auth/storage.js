import { Platform } from 'react-native';
import * as SecureStore from 'expo-secure-store';

// Token storage. On native we use the OS secure store; expo-secure-store is not
// available in a web browser, so the web target falls back to localStorage.
const isWeb = Platform.OS === 'web';

export async function getItem(key) {
  if (isWeb) {
    try { return globalThis.localStorage?.getItem(key) ?? null; } catch { return null; }
  }
  return SecureStore.getItemAsync(key);
}

export async function setItem(key, value) {
  if (isWeb) {
    try { globalThis.localStorage?.setItem(key, value); } catch { /* ignore */ }
    return;
  }
  return SecureStore.setItemAsync(key, value);
}

export async function deleteItem(key) {
  if (isWeb) {
    try { globalThis.localStorage?.removeItem(key); } catch { /* ignore */ }
    return;
  }
  return SecureStore.deleteItemAsync(key);
}
