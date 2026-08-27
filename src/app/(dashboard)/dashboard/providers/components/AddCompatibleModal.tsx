"use client";

import { Badge, Button, Input, Modal, Select } from "@/shared/components";
import {
  useAddCompatibleForm,
  API_TYPE_OPTIONS,
} from "./useAddCompatibleForm";

export interface AddCompatibleModalProps {
  variant: "openai" | "anthropic";
  isOpen: boolean;
  onClose: () => void;
  onCreated: (node: unknown) => void;
}

function AddCompatibleModal({
  variant,
  isOpen,
  onClose,
  onCreated,
}: AddCompatibleModalProps) {
  const {
    config,
    formData,
    updateFormField,
    submitting,
    checkKey,
    setCheckKey,
    checkModelId,
    setCheckModelId,
    validating,
    validationResult,
    handleSubmit,
    handleValidate,
  } = useAddCompatibleForm({ variant, isOpen, onCreated });

  const renderValidationResult = () => {
    if (!validationResult) return null;
    const { valid, error, method } = validationResult;
    if (valid) {
      return (
        <>
          <Badge variant="success">Valid</Badge>
          {method === "chat" && (
            <span className="text-sm text-text-muted">
              (via inference test)
            </span>
          )}
        </>
      );
    }
    return (
      <div className="flex flex-col gap-1">
        <Badge variant="error">Invalid</Badge>
        {error && <span className="text-sm text-red-500">{error}</span>}
      </div>
    );
  };

  return (
    <Modal isOpen={isOpen} title={config.title} onClose={onClose}>
      <div className="flex flex-col gap-4">
        <Input
          label="Name"
          value={formData.name}
          onChange={(e) => updateFormField("name", e.target.value)}
          placeholder={config.namePlaceholder}
          hint="Required. A friendly label for this node."
        />
        <Input
          label="Prefix"
          value={formData.prefix}
          onChange={(e) => updateFormField("prefix", e.target.value)}
          placeholder={config.prefixPlaceholder}
          hint="Required. Used as the provider prefix for model IDs."
        />
        {config.hasApiType && (
          <Select
            label="API Type"
            options={API_TYPE_OPTIONS}
            value={formData.apiType ?? "chat"}
            onChange={(e) => updateFormField("apiType", e.target.value)}
          />
        )}
        <Input
          label="Base URL"
          value={formData.baseUrl}
          onChange={(e) => updateFormField("baseUrl", e.target.value)}
          placeholder={config.defaultBaseUrl}
          hint={config.baseUrlHint}
        />
        <Input
          label="API Key (for Check)"
          type="password"
          value={checkKey}
          onChange={(e) => setCheckKey(e.target.value)}
        />
        <Input
          label="Model ID (optional)"
          value={checkModelId}
          onChange={(e) => setCheckModelId(e.target.value)}
          placeholder={config.modelIdPlaceholder}
          hint="If provider lacks /models endpoint, enter a model ID to validate via chat/completions instead."
        />
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
          <Button
            onClick={handleValidate}
            disabled={!checkKey || validating || !formData.baseUrl.trim()}
            variant="secondary"
            className="w-full sm:w-auto"
          >
            {validating ? "Checking..." : "Check"}
          </Button>
          {renderValidationResult()}
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
          <Button
            onClick={handleSubmit}
            fullWidth
            disabled={
              !formData.name.trim() ||
              !formData.prefix.trim() ||
              !formData.baseUrl.trim() ||
              submitting
            }
          >
            {submitting ? "Creating..." : "Create"}
          </Button>
          <Button onClick={onClose} variant="ghost" fullWidth>
            Cancel
          </Button>
        </div>
      </div>
    </Modal>
  );
}

export default AddCompatibleModal;
