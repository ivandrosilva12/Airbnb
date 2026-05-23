import { useMemo } from 'react';
import { useAuth } from '../auth/AuthContext';
import { createApi } from './client';

// useApi returns an API client wired to the current auth session.
export function useApi() {
  const { getAccessToken } = useAuth();
  return useMemo(() => createApi(getAccessToken), [getAccessToken]);
}
