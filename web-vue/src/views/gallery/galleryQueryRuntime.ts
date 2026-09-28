import { computed, ref, watch, type Ref } from 'vue'

import { galleryApi, type GalleryFile, type ImageStorageStats } from '@/api/gallery'
import type { PageRuntime } from '@/composables/usePageRuntime'
import { usePageDebouncedAction, usePagedQuery } from '@/composables/usePageQuery'
import { getNumberPreference, preferenceKeys, setNumberPreference } from '@/lib/preferences'
import {
  buildGalleryMetricItems,
  buildTagOptions,
  formatPaginationSummary,
  galleryPageSizeOptions,
} from '@/views/gallery/galleryView'

type Toast = {
  error: (message: string, title?: string) => void
}

type GalleryQueryRuntimeOptions = {
  runtime: PageRuntime
  toast: Toast
  storageStats: Ref<ImageStorageStats | null>
  onApplied?: () => void
}

const LOAD_REQUEST_KEY = 'gallery:load'
const SEARCH_TIMER_KEY = 'gallery:search'

export function useGalleryQueryRuntime(options: GalleryQueryRuntimeOptions) {
  const files = ref<GalleryFile[]>([])
  const totalSize = ref(0)
  const isLoading = ref(true)
  const hasLoadedOnce = ref(false)
  const galleryLoadError = ref('')
  const tagFilter = ref('all')
  const searchQuery = ref('')
  const startDate = ref('')
  const endDate = ref('')
  const pageSize = ref(getNumberPreference(preferenceKeys.galleryPageSize, 24, { allowed: galleryPageSizeOptions }))
  const counts = ref({ all: 0, image: 0, video: 0, music: 0 })
  const allTags = ref<string[]>([])

  const tagOptions = computed(() => buildTagOptions(allTags.value))

  // 只在首次或手动刷新时拉取 tags，避免每次搜索都触发
  let tagsLoaded = false
  const galleryQuery = usePagedQuery({
    runtime: options.runtime,
    key: LOAD_REQUEST_KEY,
    pageSize,
    loading: isLoading,
    error: galleryLoadError,
    errorMessage: '加载图片管理失败',
    fetch: ({ page, pageSize: size }) => {
      const filesPromise = galleryApi.getFiles({
        page: Number(size) ? page : 1,
        page_size: Number(size),
        media_type: 'all',
        tag: tagFilter.value,
        search: searchQuery.value,
        start_date: startDate.value,
        end_date: endDate.value,
      })
      const tagsPromise = tagsLoaded
        ? Promise.resolve(allTags.value)
        : galleryApi.getTags().then((tags) => {
            tagsLoaded = true
            return tags
          })
          .catch(() => allTags.value)
      return Promise.all([filesPromise, tagsPromise])
    },
    resolvePage: ([data]) => data.page,
    resolvePageCount: ([data]) => data.page_count,
    resolveTotal: ([data]) => data.total,
    apply: ([data, tags]) => {
      files.value = data.files
      totalSize.value = data.total_size
      counts.value = data.counts
      allTags.value = tags || []
      options.onApplied?.()
    },
    onError: (message) => {
      options.toast.error(message, '加载失败')
    },
    onSettled: (latest) => {
      if (latest) hasLoadedOnce.value = true
    },
  })

  const currentPage = galleryQuery.currentPage
  const pageCount = galleryQuery.pageCount
  const totalItems = galleryQuery.total
  const paginationSummary = computed(() => formatPaginationSummary(
    galleryQuery.paginationSummary.value.page,
    galleryQuery.paginationSummary.value.pageCount,
    galleryQuery.paginationSummary.value.total,
  ))
  const galleryMetricItems = computed(() => buildGalleryMetricItems(
    totalItems.value,
    options.storageStats.value,
    counts.value,
    totalSize.value,
  ))
  const searchDebounce = usePageDebouncedAction({
    runtime: options.runtime,
    key: SEARCH_TIMER_KEY,
    delayMs: 250,
    action: () => galleryQuery.resetAndLoad(),
  })

  async function loadGallery() {
    await galleryQuery.load()
  }

  async function refreshAll() {
    // 手动刷新时重置 tags 缓存，确保新增标签能同步
    tagsLoaded = false
    await loadGallery()
  }

  function resetAndLoad() {
    galleryQuery.resetAndLoad()
  }

  function invalidate() {
    galleryQuery.invalidate()
  }

  function clearTimers() {
    searchDebounce.clear()
  }

  watch([tagFilter, startDate, endDate, pageSize], () => {
    if (!options.runtime.isActive.value) return
    resetAndLoad()
  })

  watch(pageSize, (value) => {
    setNumberPreference(preferenceKeys.galleryPageSize, value)
  })

  watch(searchQuery, () => {
    searchDebounce.schedule()
  })

  return {
    files,
    totalSize,
    isLoading,
    hasLoadedOnce,
    galleryLoadError,
    tagFilter,
    searchQuery,
    startDate,
    endDate,
    pageSize,
    counts,
    allTags,
    tagOptions,
    currentPage,
    pageCount,
    totalItems,
    paginationSummary,
    galleryMetricItems,
    loadGallery,
    refreshAll,
    resetAndLoad,
    invalidate,
    clearTimers,
  }
}
