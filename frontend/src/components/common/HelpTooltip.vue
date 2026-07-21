<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, useTemplateRef, nextTick } from 'vue'

const props = withDefaults(defineProps<{
  content?: string
  trigger?: 'hover' | 'click'
  widthClass?: string
}>(), {
  trigger: 'hover',
  widthClass: 'w-64',
})

const show = ref(false)
const triggerRef = useTemplateRef<HTMLElement>('trigger')
const tooltipRef = useTemplateRef<HTMLElement>('tooltip')
const placement = ref<'top' | 'bottom'>('top')
const tooltipStyle = ref({ top: '0px', left: '0px' })
const tooltipContentStyle = ref({ maxHeight: 'none' })

const tooltipGap = 8
const viewportPadding = 8

function openTooltip() {
  show.value = true
  nextTick(updatePosition)
}

function closeTooltip() {
  show.value = false
}

function onEnter() {
  if (props.trigger !== 'hover') return
  openTooltip()
}

function onLeave() {
  if (props.trigger !== 'hover') return
  closeTooltip()
}

function onFocusIn() {
  if (props.trigger !== 'hover') return
  openTooltip()
}

function onFocusOut(event: FocusEvent) {
  if (props.trigger !== 'hover') return
  const nextTarget = event.relatedTarget as Node | null
  if (nextTarget && triggerRef.value?.contains(nextTarget)) return
  closeTooltip()
}

function onClick(event: MouseEvent) {
  if (props.trigger !== 'click') return
  event.stopPropagation()
  if (show.value) {
    closeTooltip()
    return
  }
  openTooltip()
}

function onDocumentClick(event: MouseEvent) {
  if (props.trigger !== 'click' || !show.value) return
  const target = event.target as Node | null
  if (!target) return
  if (triggerRef.value?.contains(target) || tooltipRef.value?.contains(target)) return
  closeTooltip()
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (props.trigger !== 'click') return
  if (event.key === 'Escape') {
    closeTooltip()
  }
}

function onViewportChange() {
  if (!show.value) return
  updatePosition()
}

function updatePosition() {
  const trigger = triggerRef.value
  const tooltip = tooltipRef.value
  if (!trigger || !tooltip) return

  const triggerRect = trigger.getBoundingClientRect()
  const tooltipRect = tooltip.getBoundingClientRect()
  const spaceAbove = triggerRect.top - tooltipGap - viewportPadding
  const spaceBelow = window.innerHeight - triggerRect.bottom - tooltipGap - viewportPadding

  if (tooltipRect.height <= spaceAbove) {
    placement.value = 'top'
  } else if (tooltipRect.height <= spaceBelow) {
    placement.value = 'bottom'
  } else {
    placement.value = spaceBelow >= spaceAbove ? 'bottom' : 'top'
  }

  const tooltipHalfWidth = tooltipRect.width / 2
  const minCenter = viewportPadding + tooltipHalfWidth
  const maxCenter = window.innerWidth - viewportPadding - tooltipHalfWidth
  const desiredCenter = triggerRect.left + triggerRect.width / 2
  const left = minCenter <= maxCenter
    ? Math.min(Math.max(desiredCenter, minCenter), maxCenter)
    : window.innerWidth / 2
  const availableHeight = placement.value === 'top' ? spaceAbove : spaceBelow

  tooltipStyle.value = {
    top: placement.value === 'top'
      ? `${triggerRect.top - tooltipGap}px`
      : `${triggerRect.bottom + tooltipGap}px`,
    left: `${left}px`,
  }
  tooltipContentStyle.value = {
    maxHeight: `${Math.max(availableHeight - 24, 0)}px`,
  }
}

onMounted(() => {
  document.addEventListener('click', onDocumentClick, true)
  document.addEventListener('keydown', onDocumentKeydown)
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('scroll', onViewportChange, true)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocumentClick, true)
  document.removeEventListener('keydown', onDocumentKeydown)
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('scroll', onViewportChange, true)
})
</script>

<template>
  <div
    ref="trigger"
    class="group relative ml-1 inline-flex items-center align-middle"
    @mouseenter="onEnter"
    @mouseleave="onLeave"
    @focusin="onFocusIn"
    @focusout="onFocusOut"
    @click="onClick"
  >
    <!-- Trigger Icon -->
    <slot name="trigger">
      <svg
        class="h-4 w-4 cursor-help text-gray-400 transition-colors hover:text-primary-600 dark:text-gray-500 dark:hover:text-primary-400"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
        />
      </svg>
    </slot>

    <!-- Teleport to body to escape modal overflow clipping -->
    <Teleport to="body">
      <div
        ref="tooltip"
        v-show="show"
        role="tooltip"
        :data-placement="placement"
        :class="[
          'fixed z-[99999] -translate-x-1/2 rounded-lg bg-gray-900 p-3 text-xs leading-relaxed text-white shadow-xl ring-1 ring-white/10 dark:bg-gray-800',
          placement === 'top' ? '-translate-y-full' : '',
          props.widthClass,
        ]"
        :style="tooltipStyle"
      >
        <button
          v-if="props.trigger === 'click'"
          type="button"
          class="absolute right-1.5 top-1.5 rounded p-1 text-gray-300 transition-colors hover:bg-white/10 hover:text-white"
          aria-label="Close"
          @click.stop="closeTooltip"
        >
          <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
        <div class="overflow-y-auto" :style="tooltipContentStyle">
          <slot>{{ content }}</slot>
        </div>
        <div
          :class="[
            'absolute left-1/2 h-2 w-2 -translate-x-1/2 rotate-45 bg-gray-900 dark:bg-gray-800',
            placement === 'top' ? '-bottom-1' : '-top-1'
          ]"
        ></div>
      </div>
    </Teleport>
  </div>
</template>
