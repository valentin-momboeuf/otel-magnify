import { useQuery } from '@tanstack/react-query'
import { featuresAPI } from '../api/admin'
import { featuresKeys } from '../api/queryKeys'

export function useFeatures() {
  return useQuery({
    queryKey: featuresKeys.all,
    queryFn: featuresAPI.get,
    staleTime: Infinity,
  })
}

export function useFeature(flag: string): boolean {
  const { data } = useFeatures()
  return data?.[flag] === true
}
