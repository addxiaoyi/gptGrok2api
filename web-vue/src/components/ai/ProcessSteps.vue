<template>
  <div class="process-steps">
    <div
      v-for="(step, index) in steps"
      :key="index"
      class="process-step"
      :class="{
        'process-step--active': step.status === 'active',
        'process-step--completed': step.status === 'completed',
        'process-step--error': step.status === 'error',
        'process-step--pending': step.status === 'pending',
        'process-step--vertical': vertical,
      }"
    >
      <div class="process-step__header">
        <div class="process-step__icon">
          <Icon
            v-if="step.status === 'completed'"
            icon="lucide:check"
            class="h-4 w-4"
          />
          <Icon
            v-else-if="step.status === 'error'"
            icon="lucide:x-circle"
            class="h-4 w-4"
          />
          <Icon
            v-else-if="step.status === 'active'"
            icon="lucide:loader-circle"
            class="h-4 w-4 animate-spin"
          />
          <span
            v-else
            class="process-step__number"
          >{{ index + 1 }}</span>
        </div>
        <div class="process-step__content">
          <p class="process-step__title">{{ step.title }}</p>
          <p v-if="step.description" class="process-step__description">{{ step.description }}</p>
        </div>
        <div v-if="step.action" class="process-step__action">
          <slot name="action" :step="step" :index="index">
            <Button
              v-if="step.action"
              size="xs"
              variant="outline"
              @click="$emit('step-click', step)"
            >
              {{ step.action }}
            </Button>
          </slot>
        </div>
      </div>
      <div
        v-if="index < steps.length - 1"
        class="process-step__connector"
        :class="{
          'process-step__connector--vertical': vertical,
        }"
      ></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Icon } from '@iconify/vue'
import { Button } from 'nanocat-ui'

withDefaults(defineProps<{
  steps: {
    title: string
    description?: string
    status: 'pending' | 'active' | 'completed' | 'error'
    action?: string
  }[]
  vertical?: boolean
}>(), {
  vertical: false,
})

defineEmits<{
  'step-click': [step: { title: string; status: string; action?: string }]
}>()
</script>

<style scoped>
.process-steps {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.process-step {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  padding: 0.75rem;
  border-radius: 8px;
  transition: background-color 150ms ease;
}

.process-step--vertical {
  flex-direction: column;
}

.process-step:hover {
  background: hsl(var(--muted) / 0.5);
}

.process-step--active {
  background: hsl(var(--primary) / 0.05);
}

.process-step--completed {
  opacity: 0.9;
}

.process-step--error {
  background: hsl(var(--destructive) / 0.05);
}

.process-step__header {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  flex: 1;
}

.process-step--vertical .process-step__header {
  flex-direction: column;
  align-items: flex-start;
}

.process-step__icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 2rem;
  height: 2rem;
  border-radius: 50%;
  background: hsl(var(--muted));
  color: hsl(var(--muted-foreground));
  flex-shrink: 0;
  font-size: 0.75rem;
  font-weight: 600;
}

.process-step--active .process-step__icon {
  background: hsl(var(--primary));
  color: hsl(var(--primary-foreground));
}

.process-step--completed .process-step__icon {
  background: hsl(var(--success) || #10b981);
  color: white;
}

.process-step--error .process-step__icon {
  background: hsl(var(--destructive));
  color: white;
}

.process-step__number {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 100%;
}

.process-step__content {
  flex: 1;
  min-width: 0;
}

.process-step--vertical .process-step__content {
  margin-left: 0;
}

.process-step__title {
  font-size: 0.875rem;
  font-weight: 500;
  color: hsl(var(--foreground));
  margin: 0;
}

.process-step__description {
  font-size: 0.75rem;
  color: hsl(var(--muted-foreground));
  margin-top: 0.25rem;
  margin-bottom: 0;
}

.process-step__action {
  flex-shrink: 0;
}

.process-step__connector {
  height: 1.5rem;
  width: 2px;
  margin-left: 1rem;
  background: hsl(var(--border));
  flex-shrink: 0;
}

.process-step__connector--vertical {
  height: 0.5rem;
  width: 100%;
  margin-left: 0;
  margin-top: 0.5rem;
}
</style>
