<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 48 48"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    aria-hidden="true"
  >
    <!-- Leaf + stem -->
    <path d="M24 9.5C24 6.5 25.5 4.2 27.2 3.2" stroke="#5A4028" stroke-width="2" stroke-linecap="round" />
    <path
      d="M23.5 8.5C18.5 8.2 14.8 6.4 13.8 3.8C17.8 2.8 21.8 4.4 23.5 8.5Z"
      fill="#769A3B"
      stroke="#4B7026"
      stroke-width="1.3"
      stroke-linejoin="round"
    />
    <!-- Outer avocado peel -->
    <path
      d="M24 9C17.2 9 11 19.6 11 29.2C11 38.2 16.8 44 24 44C31.2 44 37 38.2 37 29.2C37 19.6 30.8 9 24 9Z"
      fill="#4B7026"
    />
    <!-- Inner butter-lime flesh -->
    <path
      d="M24 12C18.7 12 13.8 20.6 13.8 29C13.8 36.2 18.4 41 24 41C29.6 41 34.2 36.2 34.2 29C34.2 20.6 29.3 12 24 12Z"
      fill="#F1F3A6"
    />
    <!-- Gentle eyes & smile -->
    <circle cx="20.4" cy="20.5" r="1.25" fill="#2D241C" />
    <circle cx="27.6" cy="20.5" r="1.25" fill="#2D241C" />
    <circle cx="18.3" cy="22.1" r="1.4" fill="#F59E7C" fill-opacity="0.7" />
    <circle cx="29.7" cy="22.1" r="1.4" fill="#F59E7C" fill-opacity="0.7" />
    <path d="M21.8 22.8C23 24 25 24 26.2 22.8" stroke="#2D241C" stroke-width="1.35" stroke-linecap="round" />
    <!-- Warm baby-pit nest -->
    <circle cx="24" cy="31.8" r="5.6" fill="#C9733F" />
    <circle cx="22.5" cy="30.4" r="1.7" fill="#E39765" fill-opacity="0.55" />
  </svg>
</template>

<script setup>
defineProps({
  size: {
    type: [Number, String],
    default: 36,
  },
})
</script>
